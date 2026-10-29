"A retained value and revision on a collaboration session's named object board."
class SharedPublication [
    | name value revision participant present |
    initialize [ name := ''. value := nil. revision := 0. participant := nil. present := false ]
    name: label [ name := label. ^ self ]
    name [ ^ name ]
    value [ ^ value ]
    revision [ ^ revision ]
    participant [ ^ participant ]
    present [ ^ present ]
    value: newValue by: author [ value := newValue. participant := author. revision := revision + 1. present := true. ^ self ]
    removeBy: author [ value := nil. participant := author. revision := revision + 1. present := false. ^ self ]
]
