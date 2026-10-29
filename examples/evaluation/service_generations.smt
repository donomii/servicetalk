"Two generations behind one stable Service address."
service := Service new.
service name: 'Greeting'.
first := service prepareSource: 'class Greeting [ hello [ ^ ''Hello from v1'' ] ]'.
service check: first selector: 'hello' arguments: {} expect: 'Hello from v1'.
service promote: first.
old := service current.
second := service prepareSource: 'class Greeting [ hello [ ^ ''Hello from v2'' ] ]'.
service check: second selector: 'hello' arguments: {} expect: 'Hello from v2'.
service promote: second.
(service call: 'hello' arguments: {}) = 'Hello from v2' ifFalse: ['new routing failed' signal].
old hello = 'Hello from v1' ifFalse: ['old reference changed' signal].
(service targetFor: first) isNil ifFalse: ['drained target still owned by service' signal].
service status
