"Dog — demonstrates the Smalltalk-syntax front end.  The evaluator is shared
 with the S-expression syntax, so this class is interchangeable with a .smt
 class of the same shape."

class Dog [
    | name breed |

    name: n breed: b [
        name := n.
        breed := b
    ]

    initialize [
        name := 'Rex'.
        breed := 'Labrador'
    ]

    name   [ ^ name ]
    breed  [ ^ breed ]

    bark [
        Transcript show: name , ' says: Woof!'
    ]

    describe [
        Transcript show: name , ' is a ' , breed
    ]
]
