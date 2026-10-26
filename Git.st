"Programmatic Git access using explicit arguments, without a shell."
class Git [
    | directory lastProcess stagingArguments invocationArguments sshEnabled sshIdentity sshAgent sshConfig |

    initialize [ directory := '.'. lastProcess := nil. sshEnabled := false. sshIdentity := ''. sshAgent := ''. sshConfig := '' ]
    directory: path [ directory := path. ^ self ]
    directory [ ^ directory ]
    lastProcess [ ^ lastProcess ]

    run: arguments [
        lastProcess := Process new.
        invocationArguments := Array new.
        sshEnabled ifTrue: [
            invocationArguments add: '-c'.
            invocationArguments add: ('core.sshCommand=' , self sshCommand).
        ] ifFalse: [ nil ].
        arguments do: [argument | invocationArguments add: argument].
        lastProcess start: 'git' args: invocationArguments cwd: directory.
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

    useSSH [ sshEnabled := true. ^ self ]
    useDefaultAuth [ sshEnabled := false. ^ self ]
    sshIdentityFile: path [ sshIdentity := path. sshEnabled := true. ^ self ]
    sshAgent: socket [ sshAgent := socket. sshEnabled := true. ^ self ]
    sshConfigFile: path [ sshConfig := path. sshEnabled := true. ^ self ]
    quoteSSH: text [ ^ '''' , (text replaceAll: '''' with: '''"''"''') , '''' ]
    quoteSSHOption: text [ ^ '"' , ((text replaceAll: '\' with: '\\') replaceAll: '"' with: '\"') , '"' ]
    sshCommand [
        ^ 'ssh -o BatchMode=yes' ,
          (sshConfig = '' ifTrue: [ '' ] ifFalse: [ ' -F ' , (self quoteSSH: sshConfig) ]) ,
          (sshIdentity = '' ifTrue: [ '' ] ifFalse: [ ' -o IdentitiesOnly=yes -i ' , (self quoteSSH: sshIdentity) ]) ,
          (sshAgent = '' ifTrue: [ '' ] ifFalse: [ ' -o ' , (self quoteSSH: ('IdentityAgent=' , (self quoteSSHOption: sshAgent))) ])
    ]
    remote: name url: url [ ^ self checked: {'remote'. 'set-url'. '--'. name. url} ]
    remote: name pushURL: url [ ^ self checked: {'remote'. 'set-url'. '--push'. '--'. name. url} ]
    checkAuth: remote [ ^ self checked: {'ls-remote'. '--'. remote} ]

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
