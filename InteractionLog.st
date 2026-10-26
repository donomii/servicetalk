class InteractionLog [
    | records retained |
    initialize [ records := Array new. retained := Array new ]
    append: record [ records add: record. ^ self ]
    retain: object [ retained add: object. ^ self ]
    entries [ ^ records ]
    size [ ^ records size ]
    clear [ records := Array new. retained := Array new. ^ self ]
]
