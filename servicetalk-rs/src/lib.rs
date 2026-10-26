//! Servicetalk Rust foreign-class runtime.
//!
//! A Rust foreign class is a Cargo crate sitting in a `<ClassName>/`
//! directory next to the servicetalk classpath.  Its `src/lib.rs`
//! exposes one `pub fn` per servicetalk method, each taking
//! `&[Value]` and returning `Value`.  The class's manifest
//! (`<ClassName>.smt`) lists `(rmethod selector rust_path)` entries
//! mapping servicetalk selectors to those functions.
//!
//! servicetalk's `BuildForeignClass` generates `src/_st_main.rs`
//! from the manifest — it builds a `dispatch(selector, args)` match
//! and calls [`main_loop`].  The result is a binary that speaks the
//! same length-prefixed stdin/stdout protocol the C runtime uses,
//! so the Go parent doesn't care which language the child is in.
//!
//! Wire format mirrors `foreign.go` and `_st_runtime.c` byte-for-byte:
//!
//! ```text
//!   parent → child:  OP_CALL  selector(u32+bytes)  nargs(u32)  args*
//!   child  → parent: OP_RETURN value     |  OP_SIGNAL msg(u32+bytes)
//!                  | OP_SEND target sel nargs args*  (then read OP_SEND_REPLY)
//!
//!   Value =
//!     1 byte kind: NIL(0x01) | BOOL(0x02) | NUMBER(0x03) | STRING(0x04) | OBJID(0x05)
//!     NIL:    no payload
//!     BOOL:   1 byte (0/1)
//!     NUMBER: 8 bytes little-endian IEEE-754
//!     STRING / OBJID: u32 length + bytes
//! ```

use std::io::{self, Read, Write};
use std::sync::Mutex;

// ---- Op codes (must match foreign.go) ----

pub const OP_CALL: u8 = 0x01;
pub const OP_RETURN: u8 = 0x02;
pub const OP_SEND: u8 = 0x03;
pub const OP_SEND_REPLY: u8 = 0x04;
pub const OP_SIGNAL: u8 = 0x05;

pub const K_NIL: u8 = 0x01;
pub const K_BOOL: u8 = 0x02;
pub const K_NUMBER: u8 = 0x03;
pub const K_STRING: u8 = 0x04;
pub const K_OBJID: u8 = 0x05;

// ---- Value ----

/// A servicetalk value crossing the wire.  Owns its bytes for
/// strings and objids; primitives are inline.
#[derive(Debug, Clone, PartialEq)]
pub enum Value {
    Nil,
    Bool(bool),
    Number(f64),
    String(Vec<u8>),
    ObjID(Vec<u8>),
}

impl Value {
    pub fn nil() -> Value { Value::Nil }
    pub fn bool(b: bool) -> Value { Value::Bool(b) }
    pub fn number(n: f64) -> Value { Value::Number(n) }
    pub fn string<S: Into<Vec<u8>>>(s: S) -> Value { Value::String(s.into()) }
    pub fn cstr(s: &str) -> Value { Value::String(s.as_bytes().to_vec()) }
    pub fn objid<S: Into<Vec<u8>>>(s: S) -> Value { Value::ObjID(s.into()) }

    pub fn is_nil(&self) -> bool { matches!(self, Value::Nil) }
    pub fn is_bool(&self) -> bool { matches!(self, Value::Bool(_)) }
    pub fn is_number(&self) -> bool { matches!(self, Value::Number(_)) }
    pub fn is_string(&self) -> bool { matches!(self, Value::String(_)) }
    pub fn is_object(&self) -> bool { matches!(self, Value::ObjID(_)) }

    pub fn as_number(&self) -> f64 {
        match self { Value::Number(n) => *n, _ => 0.0 }
    }
    pub fn as_bool(&self) -> bool {
        match self { Value::Bool(b) => *b, _ => false }
    }
    /// Returns the bytes of a string or objid value, or `&[]` for
    /// everything else.  Use [`as_str`] when you want a `&str` and
    /// are willing to panic on invalid UTF-8.
    pub fn as_bytes(&self) -> &[u8] {
        match self {
            Value::String(b) | Value::ObjID(b) => b,
            _ => &[],
        }
    }
    /// Convenience for the common case — strings and objids that are
    /// valid UTF-8.  Returns `""` for non-string-shaped values.
    /// Lossy: invalid UTF-8 is replaced with U+FFFD.
    pub fn as_str(&self) -> std::borrow::Cow<'_, str> {
        String::from_utf8_lossy(self.as_bytes())
    }
}

// ---- Signal (Smalltalk-side exception) ----

/// Raised by [`signal`] to unwind out of a user dispatch function
/// back to [`main_loop`], which forwards it to the parent as
/// `OP_SIGNAL`.  Caught via `catch_unwind`; users should not catch
/// it themselves.
#[derive(Debug)]
pub struct Signal(pub String);

/// Raise a servicetalk signal.  Unwinds the current dispatch call
/// (does NOT tear down the subprocess), and the parent's
/// `(on:do:)` handler — if any — gets the message.  Otherwise the
/// transcript logs it.
pub fn signal<S: Into<String>>(msg: S) -> ! {
    std::panic::panic_any(Signal(msg.into()))
}

// ---- Wire codec ----

fn read_exact<R: Read>(r: &mut R, n: usize) -> io::Result<Vec<u8>> {
    let mut buf = vec![0u8; n];
    r.read_exact(&mut buf)?;
    Ok(buf)
}

fn read_u32<R: Read>(r: &mut R) -> io::Result<u32> {
    let b = read_exact(r, 4)?;
    Ok(u32::from_le_bytes([b[0], b[1], b[2], b[3]]))
}

fn read_byte<R: Read>(r: &mut R) -> io::Result<u8> {
    let b = read_exact(r, 1)?;
    Ok(b[0])
}

fn read_len_string<R: Read>(r: &mut R) -> io::Result<Vec<u8>> {
    let n = read_u32(r)? as usize;
    read_exact(r, n)
}

fn read_value<R: Read>(r: &mut R) -> io::Result<Value> {
    let kind = read_byte(r)?;
    Ok(match kind {
        K_NIL => Value::Nil,
        K_BOOL => Value::Bool(read_byte(r)? != 0),
        K_NUMBER => {
            let b = read_exact(r, 8)?;
            let bits = u64::from_le_bytes([b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7]]);
            Value::Number(f64::from_bits(bits))
        }
        K_STRING => Value::String(read_len_string(r)?),
        K_OBJID => Value::ObjID(read_len_string(r)?),
        _ => Value::Nil,
    })
}

fn write_u32<W: Write>(w: &mut W, n: u32) -> io::Result<()> {
    w.write_all(&n.to_le_bytes())
}

fn write_len_string<W: Write>(w: &mut W, s: &[u8]) -> io::Result<()> {
    write_u32(w, s.len() as u32)?;
    w.write_all(s)
}

fn write_value<W: Write>(w: &mut W, v: &Value) -> io::Result<()> {
    match v {
        Value::Nil => w.write_all(&[K_NIL]),
        Value::Bool(b) => w.write_all(&[K_BOOL, if *b { 1 } else { 0 }]),
        Value::Number(n) => {
            w.write_all(&[K_NUMBER])?;
            w.write_all(&n.to_bits().to_le_bytes())
        }
        Value::String(b) => {
            w.write_all(&[K_STRING])?;
            write_len_string(w, b)
        }
        Value::ObjID(b) => {
            w.write_all(&[K_OBJID])?;
            write_len_string(w, b)
        }
    }
}

// ---- send (child → parent) ----

// Stdout is shared between the dispatch return path and `send`; both
// the dispatch loop and a user-invoked `send` write to it.  Wrap it
// in a Mutex so concurrent (e.g. spawned thread) sends don't
// interleave on the wire.  Stdin is held by main_loop while it owns
// the dispatch cycle.
static OUT_LOCK: Mutex<()> = Mutex::new(());

/// Send `selector` (with `args`) to another servicetalk object.
/// Blocks the current thread until the parent replies.  Returns the
/// reply value.
///
/// **Important**: only callable from inside a dispatch function (when
/// [`main_loop`] owns stdin).  Calling from a background thread that
/// races with `main_loop`'s next OP_CALL read produces undefined
/// behaviour.  If you need fan-out, drive everything from the
/// dispatch thread.
pub fn send(target: &Value, selector: &str, args: &[Value]) -> Value {
    let _guard = OUT_LOCK.lock().unwrap();
    let stdout = io::stdout();
    let mut out = stdout.lock();
    let stdin = io::stdin();
    let mut inp = stdin.lock();
    if write_send_to(&mut out, target, selector, args).is_err() {
        return Value::Nil;
    }
    if out.flush().is_err() {
        return Value::Nil;
    }
    let op = match read_byte(&mut inp) {
        Ok(b) => b,
        Err(_) => return Value::Nil,
    };
    if op != OP_SEND_REPLY {
        return Value::Nil;
    }
    read_value(&mut inp).unwrap_or(Value::Nil)
}

fn write_send_to<W: Write>(w: &mut W, target: &Value, selector: &str, args: &[Value]) -> io::Result<()> {
    w.write_all(&[OP_SEND])?;
    write_value(w, target)?;
    write_len_string(w, selector.as_bytes())?;
    write_u32(w, args.len() as u32)?;
    for a in args {
        write_value(w, a)?;
    }
    Ok(())
}

// ---- main loop ----

/// Dispatch function signature — one match arm per servicetalk
/// method.  Servicetalk auto-generates the body of this from the
/// class's manifest.
pub type Dispatch = fn(selector: &str, args: &[Value]) -> Value;

/// Read OP_CALL frames from stdin, hand each to `dispatch`, write
/// the result (OP_RETURN) or any raised signal (OP_SIGNAL) back to
/// stdout.  Exits cleanly on stdin EOF.
///
/// Wrap each dispatch call in `catch_unwind` so [`signal`]'s panic
/// unwinds back to here without taking down the subprocess.  Other
/// panics propagate (the subprocess dies, the parent's RPC times
/// out — same loud failure as in C with a SIGSEGV).
pub fn main_loop(dispatch: Dispatch) {
    let stdin = io::stdin();
    let stdout = io::stdout();

    loop {
        // Acquire and drop the stdin lock per frame so a user
        // `send()` from inside dispatch (which locks stdin
        // itself) doesn't have to share a BufReader cursor with
        // this loop.  Rust's Stdin lock is reentrant but layering
        // BufReader on top makes that awkward; the per-frame
        // re-lock keeps the protocol bytes flowing straight off
        // the raw Stdin and is plenty fast for an RPC subprocess.
        let op = {
            let mut inp = stdin.lock();
            match read_byte(&mut inp) {
                Ok(b) => b,
                Err(_) => return, // EOF or pipe closed
            }
        };
        if op != OP_CALL {
            return; // protocol violation — stop cleanly
        }
        let (selector, args) = {
            let mut inp = stdin.lock();
            let sel_bytes = match read_len_string(&mut inp) {
                Ok(b) => b,
                Err(_) => return,
            };
            let nargs = match read_u32(&mut inp) {
                Ok(n) => n,
                Err(_) => return,
            };
            let mut args = Vec::with_capacity(nargs as usize);
            for _ in 0..nargs {
                args.push(match read_value(&mut inp) {
                    Ok(v) => v,
                    Err(_) => return,
                });
            }
            (String::from_utf8_lossy(&sel_bytes).into_owned(), args)
        };

        let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            dispatch(&selector, &args)
        }));

        let _guard = OUT_LOCK.lock().unwrap();
        let mut out = stdout.lock();
        match result {
            Ok(v) => {
                let _ = out.write_all(&[OP_RETURN]);
                let _ = write_value(&mut out, &v);
            }
            Err(panic) => {
                let msg = if let Some(sig) = panic.downcast_ref::<Signal>() {
                    sig.0.clone()
                } else if let Some(s) = panic.downcast_ref::<String>() {
                    s.clone()
                } else if let Some(s) = panic.downcast_ref::<&'static str>() {
                    (*s).to_string()
                } else {
                    "Rust panic in dispatch".to_string()
                };
                let _ = out.write_all(&[OP_SIGNAL]);
                let _ = write_len_string(&mut out, msg.as_bytes());
            }
        }
        let _ = out.flush();
    }
}

