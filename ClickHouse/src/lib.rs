//! ClickHouse foreign class — minimal client over the HTTP interface.
//!
//! Why blocking-reqwest instead of tokio + the official `clickhouse`
//! crate?  The servicetalk dispatch path is synchronous; foreign
//! classes spin a tokio runtime when the underlying SDK is async
//! (Robocop, DynamoDB, S3), but ClickHouse over HTTP is just a POST
//! with the SQL in the request body.  `reqwest::blocking` keeps the
//! footprint tiny.
//!
//! Credentials and target URL are per-instance state — servicetalk
//! spawns a fresh subprocess per `(new ClickHouse)`, so the
//! `Mutex<Option<…>>` globals here are really per-instance fields.

use servicetalk::{signal, Value};
use std::sync::Mutex;

static URL: Mutex<Option<String>> = Mutex::new(None);
static USER: Mutex<Option<String>> = Mutex::new(None);
static PASSWORD: Mutex<Option<String>> = Mutex::new(None);
static DATABASE: Mutex<Option<String>> = Mutex::new(None);

fn url() -> String {
    URL.lock().unwrap().clone().unwrap_or_else(|| "http://localhost:8123".to_string())
}
fn user() -> Option<String> { USER.lock().unwrap().clone() }
fn password() -> Option<String> { PASSWORD.lock().unwrap().clone() }
fn database() -> Option<String> { DATABASE.lock().unwrap().clone() }

fn require_string(v: &Value, who: &str) -> String {
    if !v.is_string() {
        signal(format!("{}: expects a string", who));
    }
    String::from_utf8_lossy(v.as_bytes()).into_owned()
}

// ---- servicetalk-facing ----

pub fn initialize(_args: &[Value]) -> Value {
    Value::Nil
}

pub fn set_url(args: &[Value]) -> Value {
    *URL.lock().unwrap() = Some(require_string(&args[0], "ClickHouse setUrl:"));
    Value::Nil
}
pub fn set_user(args: &[Value]) -> Value {
    *USER.lock().unwrap() = Some(require_string(&args[0], "ClickHouse setUser:"));
    Value::Nil
}
pub fn set_password(args: &[Value]) -> Value {
    *PASSWORD.lock().unwrap() = Some(require_string(&args[0], "ClickHouse setPassword:"));
    Value::Nil
}
pub fn set_database(args: &[Value]) -> Value {
    *DATABASE.lock().unwrap() = Some(require_string(&args[0], "ClickHouse setDatabase:"));
    Value::Nil
}

/// `query: 'SELECT 1 FORMAT JSONEachRow'` — POSTs the SQL string as
/// the request body, returns the response body verbatim.  Use a
/// trailing `FORMAT JSONEachRow` (or `FORMAT JSON`, etc.) inside the
/// SQL for structured output.
pub fn query(args: &[Value]) -> Value {
    let sql = require_string(&args[0], "ClickHouse query:");
    do_request(sql.as_bytes(), None)
}

/// `exec: 'INSERT INTO t VALUES (1)'` — same wire shape as query:,
/// kept as a separate method so the Smalltalk caller can convey
/// intent.  Returns the (typically empty) response body or
/// `{"error":...}` on non-2xx.
pub fn exec_(args: &[Value]) -> Value {
    query(args)
}

/// `queryWithBody: 'INSERT INTO t FORMAT JSONEachRow' body:
/// '{"a":1}\n{"a":2}'` — the SQL goes on the URL via `?query=`,
/// the body carries the data rows.  Standard ClickHouse INSERT-
/// streaming shape.
pub fn query_with_body(args: &[Value]) -> Value {
    let sql = require_string(&args[0], "ClickHouse queryWithBody:");
    let body = require_string(&args[1], "ClickHouse queryWithBody:body:");
    do_request(body.as_bytes(), Some(sql))
}

// ---- HTTP ----

fn do_request(body: &[u8], query_param: Option<String>) -> Value {
    let client = match reqwest::blocking::Client::builder().build() {
        Ok(c) => c,
        Err(e) => return error_value(&format!("client build: {}", e)),
    };
    let mut req_url = url();
    let mut q = Vec::new();
    if let Some(db) = database() {
        q.push(format!("database={}", urlencode(&db)));
    }
    if let Some(sql) = query_param {
        q.push(format!("query={}", urlencode(&sql)));
    }
    if !q.is_empty() {
        let sep = if req_url.contains('?') { '&' } else { '?' };
        req_url.push(sep);
        req_url.push_str(&q.join("&"));
    }
    let mut rb = client.post(&req_url).body(body.to_vec());
    if let Some(u) = user() {
        rb = rb.basic_auth(u, password());
    }
    let resp = match rb.send() {
        Ok(r) => r,
        Err(e) => return error_value(&format!("send: {}", e)),
    };
    let status = resp.status();
    let text = resp.text().unwrap_or_default();
    if !status.is_success() {
        return error_value(&format!("{}: {}", status.as_u16(), text));
    }
    Value::String(text.into_bytes())
}

fn error_value(msg: &str) -> Value {
    let payload = serde_json::json!({ "error": msg });
    Value::String(payload.to_string().into_bytes())
}

// urlencode — minimal percent-encode for query-string components.
// Encodes everything that isn't alphanum / `-`/`_`/`.`/`~`.
fn urlencode(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for b in s.bytes() {
        let safe = matches!(b, b'a'..=b'z' | b'A'..=b'Z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~');
        if safe {
            out.push(b as char);
        } else {
            out.push_str(&format!("%{:02X}", b));
        }
    }
    out
}
