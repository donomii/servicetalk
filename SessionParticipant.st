"A named human or agent with independent scratch bindings in the shared object pool."
class SessionParticipant [
    | name kind active workspace |
    initialize [ name := ''. kind := 'human'. active := true. workspace := coordinator spawn: '__workspace__' ]
    name: label kind: category [ name := label. kind := category. ^ self ]
    name [ ^ name ]
    kind [ ^ kind ]
    active [ ^ active ]
    workspace [ ^ workspace ]
    leave [ active := false. workspace := nil. ^ self ]
]
