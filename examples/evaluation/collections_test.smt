"Turn structured orders and free text into small reports."
check := EvaluationChecks new.
orders := JSON decode: '[{"name":"Ada","total":12},{"name":"Ben","total":3},{"name":"Cam","total":20}]'.
large := orders select: [:order | (order at: 'total') >= 10].
total := large inject: 0 into: [:sum :order | sum + (order at: 'total')].
check equal: total to: 32 named: 'filtered order total'.
names := large collect: [:order | order at: 'name'].
check equal: (names join: ', ') to: 'Ada, Cam' named: 'customer report'.
words := 'red blue red green blue red' splitOn: ' '.
counts := Dictionary new.
words do: [:word | counts at: word put: ((counts at: word ifAbsent: [0]) + 1)].
check equal: (counts at: 'red') to: 3 named: 'word frequency'.
check equal: (counts keys sort join: ',') to: 'blue,green,red' named: 'ordered keys'.
descending := {4. 1. 3. 2}.
descending sort: [:x :y | x > y].
check equal: (descending join: ',') to: '4,3,2,1' named: 'custom sort'.
text := 'こんにちは 🌏'.
check equal: text size to: 7 named: 'Unicode size'.
check equal: (text at: 6) to: '🌏' named: 'Unicode indexing'.
roundTrip := JSON decode: (JSON encode: counts).
check equal: (roundTrip at: 'blue') to: 2 named: 'JSON round trip'.
Transcript show: 'orders, frequencies, sorting, Unicode and JSON passed'.
JSON encode: counts.
