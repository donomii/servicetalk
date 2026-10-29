"One shared object board and participant directory in this coordinator's object pool."
class CollaborationSession [
    | participants board activity sequence transfers |
    initialize [ participants := Array new. board := Dictionary new. activity := Array new. sequence := 0. transfers := Array new ]
    participants [ ^ participants ]
    board [ ^ board ]
    activity [ ^ activity ]
    joinName: name kind: kind [
        ^ self register: SessionParticipant new name: name kind: kind
    ]
    register: participant name: name kind: kind [
        participant name: name kind: kind.
        participants add: participant.
        ^ participant
    ]
    activeParticipant: participant [
        (participants includes: participant) ifTrue: [ nil ] ifFalse: [ 'Participant does not belong to this session' signal ].
        participant active ifTrue: [ nil ] ifFalse: [ 'Participant has left this session' signal ].
        ^ participant
    ]
    leave: participant [ self activeParticipant: participant. participant leave. ^ self ]
    publish: name value: value expected: expected by: participant [
        self activeParticipant: participant.
        self validateRevision: expected.
        ^ (self entry: (board at: name ifAbsent: [ nil ]) name: name expected: expected) value: value by: participant
    ]
    validateRevision: expected [
        (expected class = 'Number') ifTrue: [ nil ] ifFalse: [ 'Publication revision must be a nonnegative integer' signal ].
        (expected < 0) ifTrue: [ 'Publication revision must be a nonnegative integer' signal ] ifFalse: [ nil ].
        (expected > 9007199254740991) ifTrue: [ 'Publication revision exceeds the exact integer limit' signal ] ifFalse: [ nil ].
        (expected floor = expected) ifTrue: [ nil ] ifFalse: [ 'Publication revision must be a nonnegative integer' signal ].
        ^ expected
    ]
    entry: entry name: name expected: expected [
        ^ entry isNil ifTrue: [ self create: name expected: expected ] ifFalse: [
            self checkRevision: entry revision expected: expected name: name.
            entry
        ]
    ]
    checkRevision: actual expected: expected name: name [
        (expected = actual) ifTrue: [ nil ] ifFalse: [ ('Publication conflict for ' , name , ': expected revision ' , expected asString , ', actual revision ' , actual asString) signal ].
        ^ actual
    ]
    create: name expected: expected [
        self checkRevision: 0 expected: expected name: name.
        ^ self install: SharedPublication new name: name
    ]
    install: entry name: name [
        entry name: name.
        board at: name put: entry.
        ^ entry
    ]
    unpublish: name expected: expected by: participant [
        self activeParticipant: participant.
        self validateRevision: expected.
        ^ (self entry: (self publication: name) name: name expected: expected) removeBy: participant
    ]
    publication: name [
        ^ self present: (board at: name ifAbsent: [ nil ]) name: name
    ]
    present: entry name: name [
        entry isNil ifTrue: [ ('No publication named ' , name) signal ] ifFalse: [ nil ].
        entry present ifTrue: [ nil ] ifFalse: [ ('Publication was removed: ' , name) signal ].
        ^ entry
    ]
    prepareBinding: name for: participant [
        self activeParticipant: participant.
        ^ self retainTransfer: {(self publication: name) value. participant workspace}
    ]
    retainTransfer: transfer [
        transfers add: transfer.
        ^ transfer
    ]
    releaseBinding: transfer [ transfers remove: transfer. ^ self ]
    record: text [
        sequence := sequence + 1.
        activity add: {sequence. text}.
        (activity size > 200) ifTrue: [ activity removeAt: 0 ] ifFalse: [ nil ].
        ^ sequence
    ]
    snapshot [ ^ self __collaborationSnapshot ]
]
