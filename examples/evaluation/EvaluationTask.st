"A small task object for exploring live state and collection queries."
class EvaluationTask [
    | title completed |
    initialize [ title := 'Untitled'. completed := false ]
    title [ ^ title ]
    title: text [ title := text. ^ self ]
    completed [ ^ completed ]
    complete [ completed := true. ^ self ]
    reopen [ completed := false. ^ self ]
    summary [ ^ title , (completed ifTrue: [' (done)'] ifFalse: [' (open)']) ]
]
