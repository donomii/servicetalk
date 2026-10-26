"An asynchronous message whose receiver, arguments and outcome remain inspectable."
class Promise [
    | state target selector arguments result error startedAt finishedAt timeoutMs |
    initialize [ state := 'idle'. result := nil. error := '' ]
    send: receiver selector: method arguments: values [ ^ self send: receiver selector: method arguments: values timeoutMs: 30000 ]
    send: receiver selector: method arguments: values timeoutMs: milliseconds [ ^ self startPromise: receiver method: method arguments: values timeoutMs: milliseconds ]
    state [ ^ state ]
    target [ ^ target ]
    selector [ ^ selector ]
    arguments [ ^ arguments ]
    error [ ^ error ]
    ready [ ^ state = 'fulfilled' or: [ state = 'rejected' ] ]
    result [ ^ self promiseResult ]
    await [ ^ self await: 30000 ]
    await: milliseconds [ ^ self awaitPromise: milliseconds ]
]
