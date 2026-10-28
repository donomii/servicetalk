"Editable documentation with its own workspace, result and executable check."
class RunnableExample [
    | title description source check workspace lastRun captured |
    initialize [ title := 'New example'. description := ''. source := 'nil'. check := ''. captured := Array new. workspace := coordinator spawn: '__workspace__' ]
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
    capture: name value: value [ captured add: {name. value}. workspace setVar: name to: value. ^ self ]
    restoreCaptured [ captured do: [ :binding | workspace setVar: (binding at: 0) to: (binding at: 1) ]. ^ self ]
    result [ ^ workspace getVar: 'it' ]
    run [
        self restoreCaptured.
        lastRun := workspace evalResult: source.
        workspace evalInternal: check.
        ^ lastRun
    ]
    reset [ workspace := coordinator spawn: '__workspace__'. self restoreCaptured. lastRun := nil. ^ self ]
    inspectorMetadata [ ^ '{"properties":[{"name":"title","getter":"title","setter":"title:","type":"string"},{"name":"description","getter":"description","setter":"description:","type":"string"},{"name":"source","getter":"source","setter":"source:","type":"string"},{"name":"check","getter":"check","setter":"check:","type":"string"}]}' ]
]
