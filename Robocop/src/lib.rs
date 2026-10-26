//! Robocop wrapper — thin servicetalk-facing veneer over the real
//! robocop crate.  The actual robots.txt parsing, fetching, caching,
//! and check logic lives in
//! `/Users/jer/exagit/monorepo-robotchecker/rust/services/robocop`;
//! this file just translates servicetalk's `Value`-shaped method
//! calls into `CheckUrlsRequest` / `CheckUrlsResponse` and drives the
//! async API from a single shared tokio runtime.
//!
//! What we link:
//!
//!   - `robocop::server::{Server, ServerHandler}` — the gRPC handler
//!     impl.  `ServerHandler::new_local(cache_dir)` is the AWS-free
//!     constructor that uses an on-disk robots cache + direct reqwest
//!     fetch.  Production with DynamoDB-backed cache would use
//!     `ServerHandler::new` instead, which needs AWS creds; that path
//!     should be a separate class.
//!   - `robocop::pb::*` (or the tonic-generated `RobocopService`
//!     trait) for the `CheckUrls{Request,Response}` types.

use once_cell::sync::OnceCell;
use servicetalk::{signal, Value};
use std::path::PathBuf;
use std::sync::Mutex;
use tokio::runtime::Runtime;

// We expose ONE Robocop instance per subprocess.  servicetalk spawns
// a fresh subprocess per `(new Robocop)`, so global state here is
// per-instance — exactly what we want.

static RUNTIME: OnceCell<Runtime> = OnceCell::new();
static CACHE_DIR: Mutex<Option<PathBuf>> = Mutex::new(None);
static SERVER: OnceCell<robocop::server::Server> = OnceCell::new();

fn runtime() -> &'static Runtime {
    RUNTIME.get_or_init(|| {
        tokio::runtime::Builder::new_multi_thread()
            .enable_all()
            .build()
            .expect("robocop wrapper: failed to build tokio runtime")
    })
}

fn server() -> &'static robocop::server::Server {
    SERVER.get_or_init(|| {
        let dir = CACHE_DIR
            .lock()
            .unwrap()
            .clone()
            .unwrap_or_else(|| PathBuf::from("/tmp/robocop-cache"));
        std::fs::create_dir_all(&dir).ok();
        let handler = robocop::server::ServerHandler::new_local(dir);
        robocop::server::Server::new(handler)
    })
}

// ---- servicetalk-facing methods ----

pub fn initialize(_args: &[Value]) -> Value {
    // Force the runtime up at init time so the first checkUrls call
    // doesn't pay the startup cost.  Don't touch the Server yet —
    // setCacheDir: must be allowed to run before the cache dir gets
    // baked in.
    runtime();
    Value::Nil
}

pub fn set_cache_dir(args: &[Value]) -> Value {
    if args.is_empty() || !args[0].is_string() {
        signal("Robocop setCacheDir: expects a string");
    }
    if SERVER.get().is_some() {
        signal("Robocop setCacheDir: must be called before the first checkUrls:");
    }
    let path = String::from_utf8_lossy(args[0].as_bytes()).into_owned();
    *CACHE_DIR.lock().unwrap() = Some(PathBuf::from(path));
    Value::Nil
}

pub fn check_urls(args: &[Value]) -> Value {
    if args.is_empty() || !args[0].is_object() {
        signal("Robocop checkUrls: expects an Array of URL strings");
    }
    let urls = pull_string_array(&args[0]);
    check_inner(urls, None)
}

pub fn check_urls_user_agent(args: &[Value]) -> Value {
    if args.len() < 2 || !args[0].is_object() || !args[1].is_string() {
        signal("Robocop checkUrls:userAgent: expects (Array, String)");
    }
    let urls = pull_string_array(&args[0]);
    let ua = String::from_utf8_lossy(args[1].as_bytes()).into_owned();
    check_inner(urls, Some(ua))
}

// ---- shared internals ----

fn check_inner(urls: Vec<String>, user_agent: Option<String>) -> Value {
    let req = robocop::pb::CheckUrlsRequest {
        urls,
        user_agent,
        // Other fields default — robocop's generated message struct
        // is `#[derive(Default)]` via prost.
        ..Default::default()
    };
    let resp = runtime().block_on(async {
        use robocop::pb::robocop_service_server::RobocopService;
        server()
            .check_urls(tonic::Request::new(req))
            .await
    });
    match resp {
        Ok(r) => {
            let inner = r.into_inner();
            // Build a JSON string the user can jsonDecode on the ST
            // side.  Returning a Smalltalk Dictionary would require
            // st_send'ing `at:put:` for every field; one JSON string
            // is the pragmatic shape.
            let payload = serde_json::json!({
                "allowed_urls": inner.allowed_urls,
                "disallowed_urls": inner.disallowed_urls,
                "error": inner.error,
                "crawl_delay_secs": inner.crawl_delay_secs,
            });
            Value::String(payload.to_string().into_bytes())
        }
        Err(status) => {
            let payload = serde_json::json!({
                "error": format!("grpc status {}: {}", status.code(), status.message()),
                "allowed_urls": Vec::<String>::new(),
                "disallowed_urls": Vec::<String>::new(),
            });
            Value::String(payload.to_string().into_bytes())
        }
    }
}

// pull_string_array — convert a servicetalk Array objid (sent over
// as Value::ObjID) into a Vec<String> by st_send'ing `size` + `at:`
// to the Array on the parent side.  N+1 RPC roundtrips, but the
// array is tiny in practice and this avoids re-implementing Array
// on the Rust side.
fn pull_string_array(arr: &Value) -> Vec<String> {
    let size = servicetalk::send(arr, "size", &[]);
    let n = size.as_number() as usize;
    let mut out = Vec::with_capacity(n);
    for i in 0..n {
        let elem = servicetalk::send(arr, "at:", &[Value::Number(i as f64)]);
        out.push(String::from_utf8_lossy(elem.as_bytes()).into_owned());
    }
    out
}
