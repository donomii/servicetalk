"An inspectable message graph. Connections and results belong to this object."
class Wiring [
    | definition retained lastRun |
    initialize [ definition := '{"nodes":[]}'. retained := Array new. lastRun := '{}' ]
    definition [ ^ definition ]
    definition: source [
        self validateWiring: source.
        retained := Array new.
        self retainWiring: source in: retained.
        definition := source.
        ^ self
    ]
    lastRun [ ^ lastRun ]
    retained [ ^ retained ]
    run [
        retained := Array new.
        self retainWiring: definition in: retained.
        ^ self executeWiring: definition retain: retained
    ]
    resume [ ^ self executeWiring: definition retain: retained previous: lastRun ]
    inspectorMetadata [ ^ '{"properties":[{"name":"definition","getter":"definition","setter":"definition:","type":"string"},{"name":"last run","getter":"lastRun","type":"string"}]}' ]
    inspectorViews [ ^ '{"views":[{"id":"wiring","title":"Message graph","body":{"kind":"column","children":[{"kind":"row","children":[{"kind":"action","text":"Run graph","method":"run","refresh":true},{"kind":"action","text":"Resume from failure","method":"resume","refresh":true}]},{"kind":"code","label":"Last run","getter":"lastRun"}]}}]}' ]
]
