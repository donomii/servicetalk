"Animal — base class for the inheritance example (see Dog.st)."

class Animal [
    | name sound |

    initialize [
        name := 'animal'.
        sound := '...'
    ]

    name [ ^ name ]

    speak [
        ^ name , ' says ' , sound
    ]

    describe [
        ^ 'I am a ' , name
    ]
]
