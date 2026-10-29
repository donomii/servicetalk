"A failed job runs its cleanup, then a caller recovers with a useful result."
check := EvaluationChecks new.
events := Array new.
result := [
    [ events add: 'start'. 'planned evaluation failure' signal. events add: 'unreachable' ]
        ensure: [events add: 'cleanup']
] onError: [:error | events add: 'caught'. error].
check equal: result to: 'planned evaluation failure' named: 'caught signal'.
check equal: (events join: ',') to: 'start,cleanup,caught' named: 'cleanup and interrupted execution'.
check equal: (6 * 7) to: 42 named: 'continued workspace'.
Transcript show: 'signals, cleanup and recovery passed'.
events join: ','.
