"Proxy — the canonical forwarding doesNotUnderstand: pattern.
 Any message Proxy doesn't implement is re-sent to its target via
 perform:, so a Proxy transparently stands in for another object.

 Note initialize is defined (even though empty): construction sends
 initialize, and without it the forwarding dNU would relay that to a
 not-yet-set target — the classic forwarding-proxy gotcha."

class Proxy [
    | target |

    initialize [ target := nil ]

    target: t [ target := t ]

    doesNotUnderstand: msg [
        ^ target perform: msg
    ]
]
