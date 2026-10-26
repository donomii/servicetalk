//! RCounter — a minimal counter whose state lives in this Rust
//! process.  From the outside this is indistinguishable from the
//! S-expression-based Counter and the C-backed CCounter: `(new
//! RCounter)` spawns this binary as a subprocess, `(send c
//! increment)` routes through the bridge, and every message is
//! dispatched to one of the functions below via the auto-generated
//! `src/_st_main.rs` glue.

use servicetalk::{signal, send, Value};
use std::sync::atomic::{AtomicU64, Ordering};

// Counter state lives in the subprocess itself — Smalltalk-side
// pulse-GC handles its lifecycle.  AtomicU64 (bit-pattern of f64) so
// the dispatcher doesn't need a Mutex; only one OP_CALL frame is in
// flight at a time, but other Rust foreign classes might call back
// from the same process and we want to stay loose.
static COUNT_BITS: AtomicU64 = AtomicU64::new(0);

fn load_count() -> f64 {
    f64::from_bits(COUNT_BITS.load(Ordering::SeqCst))
}
fn store_count(n: f64) {
    COUNT_BITS.store(n.to_bits(), Ordering::SeqCst);
}

pub fn initialize(_args: &[Value]) -> Value {
    store_count(0.0);
    Value::Nil
}

pub fn increment(_args: &[Value]) -> Value {
    let n = load_count() + 1.0;
    store_count(n);
    Value::number(n)
}

pub fn decrement(_args: &[Value]) -> Value {
    let n = load_count() - 1.0;
    store_count(n);
    Value::number(n)
}

pub fn value(_args: &[Value]) -> Value {
    Value::number(load_count())
}

pub fn add(args: &[Value]) -> Value {
    if args.is_empty() || !args[0].is_number() {
        signal("RCounter add: expects a number");
    }
    let n = load_count() + args[0].as_number();
    store_count(n);
    Value::number(n)
}

/// Given another object, return its class name by sending `class`
/// back through the bridge.  Mirrors CCounter#classOf: — exercises
/// `servicetalk::send` (child → parent) from Rust.
pub fn class_of(args: &[Value]) -> Value {
    if args.is_empty() || !args[0].is_object() {
        signal("RCounter classOf: expects an object");
    }
    send(&args[0], "class", &[])
}
