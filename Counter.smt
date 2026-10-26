(class Counter
  (vars count)

  (method initialize ()
    (set count 0))

  (method increment ()
    (set count (send count + 1)))

  (method decrement ()
    (set count (send count - 1)))

  (method add: (n)
    (set count (send count + n)))

  (method value ()
    count)

  (method printValue ()
    (print "Counter value:" count))

  (method reset ()
    (set count 0))

  (method doesNotUnderstand: (msg)
    (print "Counter: I don't understand" msg)
    nil))
