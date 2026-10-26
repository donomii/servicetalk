; RCounter — a foreign (Rust-backed) counter, exercising the Rust bridge.
; Interchangeable with the ST Counter and the C-backed CCounter for
; external callers; every send routes through the same length-prefixed
; stdin/stdout protocol used by C foreign classes.

(foreign-class RCounter
  (rmethod initialize    initialize)
  (rmethod increment     increment)
  (rmethod decrement     decrement)
  (rmethod value         value)
  (rmethod add:          add)
  (rmethod classOf:      class_of))
