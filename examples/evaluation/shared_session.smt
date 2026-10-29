"Two participant workspaces share one published Counter while retaining separate scratch variables."
session := CollaborationSession new.
human := session joinName: 'Human' kind: 'human'.
agent := session joinName: 'Agent' kind: 'agent'.
counter := human workspace evalInternal: 'counter := Counter new. scratch := 7. counter'.
publication := session publish: 'teamCounter' value: counter expected: 0 by: human.
(publication revision = 1) ifTrue: [ nil ] ifFalse: [ 'First publication should have revision 1' signal ].
transfer := session prepareBinding: 'teamCounter' for: agent.
(transfer at: 1) setVar: 'counter' to: (transfer at: 0).
session releaseBinding: transfer.
human workspace evalInternal: 'counter add: 7'.
observed := agent workspace evalInternal: 'scratch := 99. counter value'.
(observed = 7) ifTrue: [ nil ] ifFalse: [ 'Agent did not see the human update' signal ].
agent workspace evalInternal: 'counter add: 5'.
observed := human workspace evalInternal: 'counter value'.
(observed = 12) ifTrue: [ nil ] ifFalse: [ 'Human did not see the agent update' signal ].
((human workspace evalInternal: 'scratch') = 7) ifTrue: [ nil ] ifFalse: [ 'Human scratch binding changed' signal ].
((agent workspace evalInternal: 'scratch') = 99) ifTrue: [ nil ] ifFalse: [ 'Agent scratch binding changed' signal ].
Transcript show: 'Both participants see the shared counter at ' , observed asString.
counter
