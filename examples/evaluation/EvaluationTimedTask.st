"A task with an estimated duration, exercising inherited initialization."
class EvaluationTimedTask extends EvaluationTask [
    | minutes |
    initialize [ super initialize. minutes := 0 ]
    minutes [ ^ minutes ]
    minutes: value [ minutes := value. ^ self ]
    summary [ ^ super summary , ' — ' , minutes asString , ' min' ]
]
