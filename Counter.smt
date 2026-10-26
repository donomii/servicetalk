(class Counter
  (vars count)

  (method initialize ()
    (set count 0))

  (method count: (value) (set count value))
  (method inspectorViews () "{\"views\":[{\"id\":\"counter\",\"title\":\"Counter\",\"body\":{\"kind\":\"column\",\"children\":[{\"kind\":\"value\",\"label\":\"Count\",\"getter\":\"value\"},{\"kind\":\"row\",\"children\":[{\"kind\":\"action\",\"text\":\"−\",\"method\":\"decrement\",\"refresh\":true},{\"kind\":\"action\",\"text\":\"+\",\"method\":\"increment\",\"refresh\":true},{\"kind\":\"action\",\"text\":\"Reset\",\"method\":\"reset\",\"refresh\":true}]},{\"kind\":\"input\",\"label\":\"Set count\",\"getter\":\"value\",\"setter\":\"count:\",\"type\":\"number\"}]}}]}")
  (method inspectorMetadata () "{\"methods\":{\"add:\":{\"arguments\":[{\"name\":\"amount\",\"type\":\"number\"}],\"returns\":\"number\"},\"count:\":{\"arguments\":[{\"name\":\"count\",\"type\":\"number\"}],\"returns\":\"number\"}},\"properties\":[{\"name\":\"count\",\"getter\":\"value\",\"setter\":\"count:\",\"type\":\"number\"}]}")

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
