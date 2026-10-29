"Small assertions used by the executable evaluation programs."
class EvaluationChecks [
    equal: actual to: expected named: name [
        (actual = expected) ifTrue: [ ^ actual ] ifFalse: [
            (name , ': expected ' , expected printString , ', got ' , actual printString) signal
        ]
    ]
]
