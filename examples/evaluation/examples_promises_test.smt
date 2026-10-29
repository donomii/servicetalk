"An asynchronous task and a reusable example with captured inputs."
check := EvaluationChecks new.
task := EvaluationTask new; title: 'Background report'; yourself.
promise := Promise new.
promise send: task selector: 'complete' arguments: {}.
completed := promise await: 3000.
check equal: completed completed to: true named: 'asynchronous result reuse'.
check equal: promise state to: 'fulfilled' named: 'promise status'.
example := RunnableExample new.
example title: 'Captured addition'; source: 'base + 7'; check: '(it = 12) ifTrue: [true] ifFalse: [''expected twelve'' signal]'.
example capture: 'base' value: 5.
example run.
check equal: example result to: 12 named: 'captured example result'.
example reset; run.
check equal: example result to: 12 named: 'captured example after reset'.
Transcript show: 'asynchronous objects and reusable examples passed'.
example result.
