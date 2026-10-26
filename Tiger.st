"Tiger — the third level of the inheritance example.
 Tiger extends Cat, which extends Animal."

class Tiger extends Cat [
    | stripes |

    initialize [
        super initialize.
        name := 'tiger'.
        sound := 'roar'.
        stripes := 21
    ]

    stripeCount [ ^ stripes ]

    describe [
        ^ (super describe) , ' with ' , stripes asString , ' stripes'
    ]
]
