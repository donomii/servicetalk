"Counter — Smalltalk-syntax twin of Counter.smt.
 Demonstrates class definitions, instance variables, and basic method
 dispatch using the .st front end."

class Counter [
    | count |

    initialize [
        count := 0
    ]

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
