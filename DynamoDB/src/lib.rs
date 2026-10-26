//! DynamoDB foreign class — thin wrapper around aws-sdk-dynamodb.
//!
//! Credentials and region come from the standard AWS chain
//! (env vars, ~/.aws/credentials, IMDS) — same logic exa's services
//! use, because we're using the same SDK.  `setRegion:` overrides
//! the env-derived region for this subprocess only.
//!
//! Items / keys cross the bridge as JSON strings whose shape matches
//! the DynamoDB wire format ({"S":"…"}, {"N":"…"}, {"M":{...}}, etc.).
//! Conversion happens inside the wrapper via serde_json + the
//! lightweight `attr_value` helper at the bottom of this file — no
//! `serde_dynamo` dep needed for the common case.

use aws_sdk_dynamodb::types::AttributeValue;
use aws_sdk_dynamodb::Client;
use once_cell::sync::OnceCell;
use servicetalk::{signal, Value};
use std::collections::HashMap;
use std::sync::Mutex;
use tokio::runtime::Runtime;

static RUNTIME: OnceCell<Runtime> = OnceCell::new();
// CLIENT is a Mutex<Option<Client>> rather than a OnceCell so
// setRegion: can drop the cached client and force the next call to
// rebuild with the new region.  Without this the first call baked
// region in for the subprocess's lifetime and a late setRegion:
// was either silently ignored or had to raise an error — worse UX
// than just rebuilding.
static CLIENT: Mutex<Option<Client>> = Mutex::new(None);
static REGION_OVERRIDE: Mutex<Option<String>> = Mutex::new(None);

fn runtime() -> &'static Runtime {
    RUNTIME.get_or_init(|| {
        tokio::runtime::Builder::new_multi_thread()
            .enable_all()
            .build()
            .expect("DynamoDB: tokio runtime build")
    })
}

// get_client — top-level block_on to lazy-build the SDK client.
// MUST run before any per-call block_on, NOT inside one — nesting
// tokio runtimes panics with "Cannot start a runtime from within a
// runtime" (the runtime can't drive its own driver).  Pattern:
// every public method calls get_client() first, then runs its
// action inside a fresh top-level block_on.
fn get_client() -> Client {
    if let Some(c) = CLIENT.lock().unwrap().as_ref() {
        return c.clone();
    }
    let region = REGION_OVERRIDE.lock().unwrap().clone();
    let conf = runtime().block_on(async {
        let mut cfg = aws_config::defaults(aws_config::BehaviorVersion::latest());
        if let Some(r) = region {
            cfg = cfg.region(aws_sdk_dynamodb::config::Region::new(r));
        }
        cfg.load().await
    });
    let c = Client::new(&conf);
    *CLIENT.lock().unwrap() = Some(c.clone());
    c
}

fn require_string(v: &Value, who: &str) -> String {
    if !v.is_string() {
        signal(format!("{}: expects a string", who));
    }
    String::from_utf8_lossy(v.as_bytes()).into_owned()
}

// ---- methods ----

pub fn initialize(_args: &[Value]) -> Value {
    runtime();
    Value::Nil
}

pub fn set_region(args: &[Value]) -> Value {
    let r = require_string(&args[0], "DynamoDB setRegion:");
    *REGION_OVERRIDE.lock().unwrap() = Some(r);
    // Drop the cached client so the next call rebuilds with the new
    // region.  Previous calls' results are still valid — only the
    // next request and onward see the new region.
    *CLIENT.lock().unwrap() = None;
    Value::Nil
}

pub fn get_item(args: &[Value]) -> Value {
    let client = get_client();
    let table = require_string(&args[0], "DynamoDB getItem:key:");
    let key_json = require_string(&args[1], "DynamoDB getItem:key:");
    let key = match parse_attr_map(&key_json) {
        Ok(m) => m,
        Err(e) => return err(&format!("parse key: {}", e)),
    };
    let res = runtime().block_on(async {
        client.get_item().table_name(&table).set_key(Some(key)).send().await
    });
    match res {
        Ok(out) => match out.item {
            Some(m) => Value::String(attr_map_to_json(&m).to_string().into_bytes()),
            None => Value::String(b"null".to_vec()),
        },
        Err(e) => err(&format!("get_item: {}", e.into_service_error())),
    }
}

pub fn put_item(args: &[Value]) -> Value {
    let client = get_client();
    let table = require_string(&args[0], "DynamoDB putItem:item:");
    let item_json = require_string(&args[1], "DynamoDB putItem:item:");
    let item = match parse_attr_map(&item_json) {
        Ok(m) => m,
        Err(e) => return err(&format!("parse item: {}", e)),
    };
    let res = runtime().block_on(async {
        client.put_item().table_name(&table).set_item(Some(item)).send().await
    });
    match res {
        Ok(_) => Value::String(br#"{"ok":true}"#.to_vec()),
        Err(e) => err(&format!("put_item: {}", e.into_service_error())),
    }
}

pub fn delete_item(args: &[Value]) -> Value {
    let client = get_client();
    let table = require_string(&args[0], "DynamoDB deleteItem:key:");
    let key_json = require_string(&args[1], "DynamoDB deleteItem:key:");
    let key = match parse_attr_map(&key_json) {
        Ok(m) => m,
        Err(e) => return err(&format!("parse key: {}", e)),
    };
    let res = runtime().block_on(async {
        client.delete_item().table_name(&table).set_key(Some(key)).send().await
    });
    match res {
        Ok(_) => Value::String(br#"{"ok":true}"#.to_vec()),
        Err(e) => err(&format!("delete_item: {}", e.into_service_error())),
    }
}

pub fn query_(args: &[Value]) -> Value {
    let client = get_client();
    let table = require_string(&args[0], "DynamoDB query:");
    let key_cond = require_string(&args[1], "DynamoDB query:keyConditionExpression:");
    let attrs_json = require_string(&args[2], "DynamoDB query:expressionAttributeValues:");
    let attrs = match parse_attr_map(&attrs_json) {
        Ok(m) => m,
        Err(e) => return err(&format!("parse attrs: {}", e)),
    };
    let res = runtime().block_on(async {
        client
            .query()
            .table_name(&table)
            .key_condition_expression(&key_cond)
            .set_expression_attribute_values(Some(attrs))
            .send()
            .await
    });
    match res {
        Ok(out) => {
            let items: Vec<serde_json::Value> = out
                .items
                .unwrap_or_default()
                .iter()
                .map(attr_map_to_json)
                .collect();
            Value::String(serde_json::to_string(&items).unwrap().into_bytes())
        }
        Err(e) => err(&format!("query: {}", e.into_service_error())),
    }
}

pub fn scan_(args: &[Value]) -> Value {
    let client = get_client();
    let table = require_string(&args[0], "DynamoDB scan:");
    let limit = if args[1].is_number() { args[1].as_number() as i32 } else { 100 };
    let res = runtime().block_on(async {
        client.scan().table_name(&table).limit(limit).send().await
    });
    match res {
        Ok(out) => {
            let items: Vec<serde_json::Value> = out
                .items
                .unwrap_or_default()
                .iter()
                .map(attr_map_to_json)
                .collect();
            Value::String(serde_json::to_string(&items).unwrap().into_bytes())
        }
        Err(e) => err(&format!("scan: {}", e.into_service_error())),
    }
}

// ---- AttributeValue <-> JSON conversion ----

fn parse_attr_map(s: &str) -> Result<HashMap<String, AttributeValue>, String> {
    let v: serde_json::Value = serde_json::from_str(s).map_err(|e| e.to_string())?;
    let obj = v.as_object().ok_or("expected JSON object at top level")?;
    let mut out = HashMap::new();
    for (k, val) in obj {
        out.insert(k.clone(), json_to_attr(val)?);
    }
    Ok(out)
}

// json_to_attr — convert a single AttributeValue wire shape
// ({"S":...} / {"N":...} / etc.) into an SDK AttributeValue.
fn json_to_attr(v: &serde_json::Value) -> Result<AttributeValue, String> {
    let obj = v.as_object().ok_or("attribute must be a tagged object")?;
    let (tag, payload) = obj.iter().next().ok_or("empty attribute")?;
    Ok(match tag.as_str() {
        "S" => AttributeValue::S(payload.as_str().ok_or("S not a string")?.to_string()),
        "N" => AttributeValue::N(match payload {
            serde_json::Value::String(s) => s.clone(),
            serde_json::Value::Number(n) => n.to_string(),
            _ => return Err("N must be string or number".into()),
        }),
        "BOOL" => AttributeValue::Bool(payload.as_bool().ok_or("BOOL not a bool")?),
        "NULL" => AttributeValue::Null(true),
        "L" => {
            let arr = payload.as_array().ok_or("L not an array")?;
            let mut out = Vec::with_capacity(arr.len());
            for x in arr {
                out.push(json_to_attr(x)?);
            }
            AttributeValue::L(out)
        }
        "M" => {
            let obj = payload.as_object().ok_or("M not an object")?;
            let mut m = HashMap::new();
            for (k, v) in obj {
                m.insert(k.clone(), json_to_attr(v)?);
            }
            AttributeValue::M(m)
        }
        "SS" => {
            let arr = payload.as_array().ok_or("SS not an array")?;
            AttributeValue::Ss(arr.iter().filter_map(|x| x.as_str().map(String::from)).collect())
        }
        "NS" => {
            let arr = payload.as_array().ok_or("NS not an array")?;
            AttributeValue::Ns(arr.iter().filter_map(|x| x.as_str().map(String::from)).collect())
        }
        other => return Err(format!("unsupported attribute tag: {}", other)),
    })
}

fn attr_to_json(av: &AttributeValue) -> serde_json::Value {
    use serde_json::json;
    match av {
        AttributeValue::S(s) => json!({"S": s}),
        AttributeValue::N(n) => json!({"N": n}),
        AttributeValue::Bool(b) => json!({"BOOL": b}),
        AttributeValue::Null(_) => json!({"NULL": true}),
        AttributeValue::L(l) => {
            let items: Vec<serde_json::Value> = l.iter().map(attr_to_json).collect();
            json!({"L": items})
        }
        AttributeValue::M(m) => json!({"M": attr_map_to_json(m)}),
        AttributeValue::Ss(s) => json!({"SS": s}),
        AttributeValue::Ns(n) => json!({"NS": n}),
        _ => json!({"UNSUPPORTED": format!("{:?}", av)}),
    }
}

fn attr_map_to_json(m: &HashMap<String, AttributeValue>) -> serde_json::Value {
    let mut o = serde_json::Map::new();
    for (k, v) in m {
        o.insert(k.clone(), attr_to_json(v));
    }
    serde_json::Value::Object(o)
}

fn err(msg: &str) -> Value {
    Value::String(serde_json::json!({"error": msg}).to_string().into_bytes())
}
