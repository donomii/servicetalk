"Counter — Smalltalk-syntax twin of Counter.smt.
 Demonstrates class definitions, instance variables, and basic method
 dispatch using the .st front end."

class Counter [
    | count |

    initialize [
        count := 0
    ]

    count: value [ count := value. ^ count ]
    inspectorViews [ ^ '{"views":[{"id":"counter","title":"Counter","body":{"kind":"column","children":[{"kind":"value","label":"Count","getter":"value"},{"kind":"row","children":[{"kind":"action","text":"−","method":"decrement","refresh":true},{"kind":"action","text":"+","method":"increment","refresh":true},{"kind":"action","text":"Reset","method":"reset","refresh":true}]},{"kind":"input","label":"Set count","getter":"value","setter":"count:","type":"number"}]}}]}' ]
    inspectorMetadata [ ^ '{"methods":{"add:":{"arguments":[{"name":"amount","type":"number"}],"returns":"number"},"count:":{"arguments":[{"name":"count","type":"number"}],"returns":"number"}},"properties":[{"name":"count","getter":"value","setter":"count:","type":"number"}]}' ]

    increment [
        count := count + 1
    ]

    decrement [
        count := count - 1
    ]

    add: n [
        count := count + n
    ]

    value [
        ^ count
    ]

    printValue [
        Transcript show: 'Counter value: ' , count asString
    ]

    reset [
        count := 0
    ]

    doesNotUnderstand: msg [
        Transcript show: 'Counter: I don''t understand ' , msg
    ]
]
