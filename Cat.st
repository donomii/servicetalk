"Cat — demonstrates single inheritance and `super` sends.
 Cat extends Animal: it inherits `name`, `speak`, and `describe`,
 overrides `initialize` and `describe`, and uses `super` to reuse the
 parent's behaviour.  See also Animal.st."

class Cat extends Animal [
    | tricks |

    initialize [
        super initialize.           "set up Animal's fields first"
        name := 'cat'.
        sound := 'meow'.
        tricks := 0
    ]

    learnTrick [
        tricks := tricks + 1
    ]

    trickCount [ ^ tricks ]

    describe [
        "extend the inherited description rather than replace it"
        ^ (super describe) , ' that knows ' , tricks asString , ' tricks'
    ]
]
