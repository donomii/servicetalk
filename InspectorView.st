class InspectorView [
    | target definition |
    initialize [ definition := '{}' ]
    target: object [ target := object. ^ self ]
    target [ ^ target ]
    definition: json [ definition := json. ^ self ]
    definition [ ^ definition ]
    inspectorMetadata [
        ^ '{"properties":[{"name":"definition","getter":"definition","setter":"definition:","type":"string"}]}'
    ]
]
