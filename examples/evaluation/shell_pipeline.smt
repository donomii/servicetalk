"Stream literal input through sort and uniq. Requires those two command-line tools."
shell := Shell new.
shell env: 'LC_ALL' put: 'C'.
pipeline := shell pipeline: {#('sort'). #('uniq' '-c')} input: 'tea
coffee
tea
milk
' outputFile: ''.
(pipeline wait = 0) ifTrue: [ nil ] ifFalse: [ (pipeline error , pipeline stderr) signal ].
expected := '      1 coffee
      1 milk
      2 tea
'.
(pipeline stdout = expected) ifTrue: [ nil ] ifFalse: [ ('Unexpected pipeline output: ' , pipeline stdout) signal ].
((pipeline exitCodes at: 0) = 0) ifTrue: [ nil ] ifFalse: [ 'sort failed' signal ].
((pipeline exitCodes at: 1) = 0) ifTrue: [ nil ] ifFalse: [ 'uniq failed' signal ].
Transcript show: pipeline stdout.
pipeline
