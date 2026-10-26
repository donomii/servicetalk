"Programmatic Git access using explicit arguments, without a shell."
class Git [
    | directory lastProcess stagingArguments |

    initialize [ directory := '.'. lastProcess := nil ]
    directory: path [ directory := path. ^ self ]
    directory [ ^ directory ]
    lastProcess [ ^ lastProcess ]

    run: arguments [
        lastProcess := Process new.
        lastProcess start: 'git' args: arguments cwd: directory.
        lastProcess wait.
        ^ lastProcess
    ]

    checked: arguments [
        self run: arguments.
        (lastProcess exitCode = 0) ifTrue: [
            ^ lastProcess output
        ] ifFalse: [
            ('Git failed in ' , directory , ' (exit ' , lastProcess exitCode asString , '): ' , lastProcess command , ' / ' , lastProcess output) signal
        ]
    ]

    status [ ^ self checked: #('status' '--short') ]
    diff [ ^ self checked: #('diff' '--no-ext-diff') ]
    stagedDiff [ ^ self checked: #('diff' '--cached' '--no-ext-diff') ]
    head [ ^ self checked: #('rev-parse' 'HEAD') ]
    branch [ ^ self checked: #('branch' '--show-current') ]
    remotes [ ^ self checked: #('remote' '-v') ]
    log [ ^ self checked: #('log' '-10' '--oneline') ]
    fetch: remote [ ^ self checked: {'fetch'. '--'. remote} ]
    add: paths [
        stagingArguments := #('add' '--').
        paths do: [path | stagingArguments add: path].
        ^ self checked: stagingArguments
    ]
    commit: message [ ^ self checked: {'commit'. '-m'. message} ]
    push: remote branch: branchName [ ^ self checked: {'push'. '--'. remote. branchName} ]
]
