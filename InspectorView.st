class InspectorView [
    | target definition root |
    initialize [ definition := '{"body":{"kind":"column","children":[]}}'. root := nil ]
    target: object [ target := object. ^ self ]
    target [ ^ target ]
    view [ ^ self ]
    effectiveReceiver [ ^ target ]
    definition: json [ ^ self __replaceView: json ]
    definition [ ^ self __describeView ]
    root [ ^ self __viewRoot ]
    root: control [ ^ self __viewRoot: control ]
    inspectorMetadata [
        ^ '{"properties":[{"name":"definition","getter":"definition","setter":"definition:","type":"string"},{"name":"root control","getter":"root","type":"objid"}]}'
    ]
]
