"The live workbench owns its tools and runnable documentation."
class Workbench [
    | definition retained examples |
    initialize [ examples := nil. self resetTools ]
    defaultDefinition [ ^ '{"title":"servicetalk","tools":[{"id":"results","label":"Results","action":"results"},{"id":"record","label":"Record","action":"record"},{"id":"recording","label":"Recording","action":"recording"},{"id":"methods","label":"Browse methods","action":"methods"},{"id":"objects","label":"Object graph","action":"objects"},{"id":"trace","label":"Message trace","action":"trace"},{"id":"debugger","label":"Debugger","action":"debugger"},{"id":"wiring","label":"Wiring","action":"wiring"},{"id":"examples","label":"Examples","action":"examples"}]}' ]
    resetTools [ ^ self definition: self defaultDefinition ]
    definition [ ^ definition ]
    definition: source [ retained := self workbenchReferences: source. definition := source. ^ self ]
    retained [ ^ retained ]
    examples [ examples isNil ifTrue: [ examples := Array new. self populateExamples ] ifFalse: [ nil ]. ^ examples ]
    addExampleTitle: title description: summary source: source check: check [
        self examples add: ((RunnableExample new) title: title; description: summary; source: source; check: check; yourself).
        ^ examples at: (examples size - 1)
    ]
    populateExamples [
        self addExampleTitle: 'A live counter' description: 'Run this example, inspect the returned counter, and click increment or value. The check verifies its initial value.' source: 'counter := Counter new.
counter increment.
counter increment.
counter add: 10.
counter' check: '(counter value = 12) ifFalse: [ ''Counter should contain 12'' signal ]'.
        self addExampleTitle: 'Clone and compare' description: 'The array result contains the original and its clone. Inspect either counter or follow their relationship in the object graph.' source: 'original := Counter new.
original add: 7.
copy := original clone.
copy add: 5.
{original. copy}' check: '(original value = 7) ifFalse: [ ''Original changed'' signal ]. (copy value = 12) ifFalse: [ ''Clone should contain 12'' signal ]'.
        self addExampleTitle: 'Derive a prototype' description: 'Give one new counter an increment method that adds five. Its loaded behavior can be cloned again.' source: 'seed := Counter new.
derived := seed cloneWith: ''{"fields":{"count":{"kind":"number","num":20}},"methods":[{"name":"increment","source":"count := count + 5"}]}''.
derived increment.
derived' check: '(derived value = 25) ifFalse: [ ''Derived increment should produce 25'' signal ]. (seed value = 0) ifFalse: [ ''Seed changed'' signal ]'.
        self addExampleTitle: 'Connect messages' description: 'Open the returned Wiring object with Open graph. One node doubles seven; the next receives that result and turns it into a string.' source: 'graph := Wiring new.
graph definition: ''{"nodes":[{"id":"double","label":"Double seven","target":{"value":{"kind":"number","num":7}},"method":"*","args":[{"value":{"kind":"number","num":2}}],"x":60,"y":60},{"id":"text","label":"As text","target":{"from":"double"},"method":"asString","args":[],"x":350,"y":60}]}''.
graph run.
graph' check: '((graph lastRun) includes: ''completed'') ifFalse: [ ''Graph did not complete'' signal ]'.
        self addExampleTitle: 'An asynchronous result' description: 'The promise owns the message and its result. Inspect it, follow the receiver, or use its result in another operation.' source: 'counter := Counter new.
counter add: 3.
promise := Promise new.
promise send: counter selector: ''value'' arguments: #().
promise await: 3000.
promise' check: '(promise result = 3) ifFalse: [ ''Promise should return 3'' signal ]'.
        self addExampleTitle: 'Handle a signal' description: 'A handler can inspect the signal and decide what to do next. An uncaught signal fails the example and its check.' source: 'caught := false.
[ ''Example signal'' signal ] onError: [ :message | caught := true ].
caught' check: 'caught ifFalse: [ ''The handler did not run'' signal ]'.
        self addExampleTitle: 'Run an external tool' description: 'Start the Go tool with explicit arguments, then inspect the returned Process for its command, output and exit status.' source: 'tool := Process new.
tool start: ''go'' args: #(''version'') cwd: ''.''.
tool wait.
tool' check: '(tool exitCode = 0) ifFalse: [ tool output signal ]'.
        ^ self
    ]
    inspectorViews [ ^ '{"views":[{"id":"workbench","title":"Workbench","body":{"kind":"column","children":[{"kind":"textarea","label":"Tools definition","getter":"definition","setter":"definition:","type":"string"},{"kind":"action","text":"Restore default tools","method":"resetTools","refresh":true}]}}]}' ]
]
