"Editable documentation with its own workspace, result and executable check."
class RunnableExample [
    | title description source check workspace lastRun |
    initialize [ title := 'New example'. description := ''. source := 'nil'. check := ''. workspace := coordinator spawn: '__workspace__' ]
    title [ ^ title ]
    title: text [ title := text. ^ self ]
    description [ ^ description ]
    description: text [ description := text. ^ self ]
    source [ ^ source ]
    source: text [ source := text. ^ self ]
    check [ ^ check ]
    check: text [ check := text. ^ self ]
    updateTitle: name description: summary source: code check: assertion [ title := name. description := summary. source := code. check := assertion. ^ self ]
    workspace [ ^ workspace ]
    result [ ^ workspace getVar: 'it' ]
    run [
        lastRun := workspace evalResult: source.
        workspace evalInternal: check.
        ^ lastRun
    ]
    reset [ workspace := coordinator spawn: '__workspace__'. lastRun := nil. ^ self ]
    inspectorMetadata [ ^ '{"properties":[{"name":"title","getter":"title","setter":"title:","type":"string"},{"name":"description","getter":"description","setter":"description:","type":"string"},{"name":"source","getter":"source","setter":"source:","type":"string"},{"name":"check","getter":"check","setter":"check:","type":"string"}]}' ]
]
