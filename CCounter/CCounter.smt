; CCounter — a foreign (C-backed) counter, exercising the C bridge.
; Interchangeable with the ST Counter class for external callers.

(foreign-class CCounter
  (cmethod initialize    ccounter_initialize)
  (cmethod increment     ccounter_increment)
  (cmethod decrement     ccounter_decrement)
  (cmethod value         ccounter_value)
  (cmethod add:          ccounter_add)
  (cmethod classOf:      ccounter_classOf))
