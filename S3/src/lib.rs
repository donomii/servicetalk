//! S3 foreign class — thin wrapper around aws-sdk-s3.  Credentials
//! / region from the standard AWS chain (env vars, ~/.aws/credentials,
//! IMDS); `setRegion:` overrides per subprocess.
//!
//! Object bodies move in/out as servicetalk strings (byte arrays).
//! Binary payloads round-trip fine — servicetalk strings aren't
//! validated as UTF-8 on the wire — but the caller picks the
//! encoding (raw bytes / base64 / whatever) if they care about
//! interop with non-ST consumers.

use aws_sdk_s3::primitives::ByteStream;
use aws_sdk_s3::Client;
use once_cell::sync::OnceCell;
use servicetalk::{signal, Value};
use std::sync::Mutex;
use tokio::runtime::Runtime;

static RUNTIME: OnceCell<Runtime> = OnceCell::new();
// CLIENT is a Mutex<Option<Client>> so setRegion: can drop the
// cached client and force the next call to rebuild with the new
// region.  See DynamoDB wrapper for the rationale.
static CLIENT: Mutex<Option<Client>> = Mutex::new(None);
static REGION_OVERRIDE: Mutex<Option<String>> = Mutex::new(None);

fn runtime() -> &'static Runtime {
    RUNTIME.get_or_init(|| {
        tokio::runtime::Builder::new_multi_thread()
            .enable_all()
            .build()
            .expect("S3: tokio runtime build")
    })
}

// get_client — top-level block_on to lazy-build the SDK client.
// Pattern: every method calls get_client() FIRST (outside any
// other block_on), then runs its action inside a fresh top-level
// block_on.  Nested block_on panics with "Cannot start a runtime
// from within a runtime".
fn get_client() -> Client {
    if let Some(c) = CLIENT.lock().unwrap().as_ref() {
        return c.clone();
    }
    let region = REGION_OVERRIDE.lock().unwrap().clone();
    let conf = runtime().block_on(async {
        let mut cfg = aws_config::defaults(aws_config::BehaviorVersion::latest());
        if let Some(r) = region {
            cfg = cfg.region(aws_sdk_s3::config::Region::new(r));
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
    let r = require_string(&args[0], "S3 setRegion:");
    *REGION_OVERRIDE.lock().unwrap() = Some(r);
    // Drop the cached client so the next call rebuilds with the
    // new region.  Callable at any time — no more "must be called
    // before the first request" footgun.
    *CLIENT.lock().unwrap() = None;
    Value::Nil
}

pub fn get_object(args: &[Value]) -> Value {
    let client = get_client();
    let bucket = require_string(&args[0], "S3 getObject:key:");
    let key = require_string(&args[1], "S3 getObject:key:");
    let res = runtime().block_on(async {
        let out = client.get_object().bucket(&bucket).key(&key).send().await?;
        let bytes = out.body.collect().await?.into_bytes();
        Ok::<_, anyhow::Error>(bytes.to_vec())
    });
    match res {
        Ok(b) => Value::String(b),
        Err(e) => err(&format!("get_object: {}", e)),
    }
}

pub fn put_object(args: &[Value]) -> Value {
    let client = get_client();
    let bucket = require_string(&args[0], "S3 putObject:key:body:");
    let key = require_string(&args[1], "S3 putObject:key:body:");
    if !args[2].is_string() {
        signal("S3 putObject:key:body: body must be a string");
    }
    let body = args[2].as_bytes().to_vec();
    let res = runtime().block_on(async {
        client
            .put_object()
            .bucket(&bucket)
            .key(&key)
            .body(ByteStream::from(body))
            .send()
            .await
    });
    match res {
        Ok(_) => Value::String(br#"{"ok":true}"#.to_vec()),
        Err(e) => err(&format!("put_object: {}", e.into_service_error())),
    }
}

pub fn delete_object(args: &[Value]) -> Value {
    let client = get_client();
    let bucket = require_string(&args[0], "S3 deleteObject:key:");
    let key = require_string(&args[1], "S3 deleteObject:key:");
    let res = runtime().block_on(async {
        client.delete_object().bucket(&bucket).key(&key).send().await
    });
    match res {
        Ok(_) => Value::String(br#"{"ok":true}"#.to_vec()),
        Err(e) => err(&format!("delete_object: {}", e.into_service_error())),
    }
}

pub fn list_objects(args: &[Value]) -> Value {
    let client = get_client();
    let bucket = require_string(&args[0], "S3 listObjects:prefix:");
    let prefix = if args[1].is_string() {
        Some(require_string(&args[1], "S3 listObjects:prefix:"))
    } else {
        None
    };
    let res = runtime().block_on(async {
        let mut req = client.list_objects_v2().bucket(&bucket);
        if let Some(p) = prefix {
            req = req.prefix(p);
        }
        req.send().await
    });
    match res {
        Ok(out) => {
            let keys: Vec<String> = out
                .contents
                .unwrap_or_default()
                .into_iter()
                .filter_map(|o| o.key)
                .collect();
            Value::String(serde_json::to_string(&keys).unwrap().into_bytes())
        }
        Err(e) => err(&format!("list_objects: {}", e.into_service_error())),
    }
}

fn err(msg: &str) -> Value {
    Value::String(serde_json::json!({"error": msg}).to_string().into_bytes())
}
