"A live shell session. Paths, environment overrides and jobs belong to this object."
class Shell [
    | cwd environment jobs lastProcess |
    initialize [ cwd := '.' absolutePath. environment := Dictionary new. jobs := Array new. lastProcess := nil ]
    pwd [ ^ cwd ]
    cwd [ ^ cwd ]
    home [ ^ self __shell: 'home' arguments: {self env: 'HOME'. self env: 'USERPROFILE'} ]
    resolvePath: path [ ^ self __shell: 'resolve' arguments: {cwd. path. self env: 'HOME'. self env: 'USERPROFILE'} ]
    cd: path [ cwd := self __shell: 'directory' arguments: {self resolvePath: path}. ^ cwd ]
    ls [ ^ self ls: '.' ]
    ls: path [ ^ self __shell: 'list' arguments: {self resolvePath: path} ]
    cat: path [ ^ self __shell: 'read' arguments: {self resolvePath: path} ]
    write: path text: text [ ^ self __shell: 'write' arguments: {self resolvePath: path. text} ]
    append: path text: text [ ^ self __shell: 'append' arguments: {self resolvePath: path. text} ]
    mkdir: path [ ^ self __shell: 'mkdir' arguments: {self resolvePath: path} ]
    find: pattern [ ^ self __shell: 'find' arguments: {cwd. pattern} ]
    grep: text in: path [ ^ self __shell: 'grep' arguments: {self resolvePath: path. text} ]
    env: name [ ^ environment at: name ifAbsent: [ name getenv ] ]
    getenv: name [ ^ self env: name ]
    env: name put: value [
        self __shell: 'environment' arguments: {name. value}.
        environment at: name put: value. ^ self
    ]
    unsetEnv: name [
        self __shell: 'environment' arguments: {name. nil}.
        environment at: name put: nil. ^ self
    ]
    environment [ ^ environment ]
    jobs [ ^ jobs ]
    lastProcess [ ^ lastProcess ]
    prepareProcess: input [
        lastProcess := Process new.
        lastProcess environment: environment.
        lastProcess input: input.
        jobs add: lastProcess.
        ^ lastProcess
    ]
    exec: program args: arguments [ ^ self exec: program args: arguments input: '' ]
    exec: program args: arguments input: input [
        self prepareProcess: input.
        lastProcess start: program args: arguments cwd: cwd.
        ^ lastProcess
    ]
    run: program args: arguments [ ^ self run: program args: arguments input: '' ]
    run: program args: arguments input: input [
        self exec: program args: arguments input: input.
        lastProcess wait.
        ^ lastProcess
    ]
    exec: command [
        self prepareProcess: ''.
        lastProcess start: command cwd: cwd.
        ^ lastProcess
    ]
    run: command [
        self exec: command.
        lastProcess wait.
        ^ lastProcess output
    ]
    checked: program args: arguments [
        self run: program args: arguments.
        (lastProcess exitCode = 0) ifTrue: [ ^ lastProcess stdout ] ifFalse: [
            ('Command failed in ' , cwd , ' (exit ' , lastProcess exitCode asString , '): ' , lastProcess command , ' / ' , lastProcess error , lastProcess stderr) signal
        ]
    ]
    help [ ^ 'Shell new creates a session. cd: changes its directory; ls lists names; cat: reads; write:text: and append:text: save text; mkdir: creates directories; find: matches names recursively; grep:in: finds text. run:args: returns a Process with stdout, stderr and exitCode. exec:args: starts a background job; jobs lists this session''s jobs. env:put: sets an environment override. run:args:input: passes text to stdin. Methods and Workspace accept normal Smalltalk expressions.' ]
    inspectorMetadata [ ^ '{"properties":[{"name":"Directory","getter":"pwd","setter":"cd:","type":"string"}],"methods":{"cd:":{"arguments":[{"name":"path","type":"string"}],"returns":"string"},"cat:":{"arguments":[{"name":"path","type":"string"}],"returns":"string"},"find:":{"arguments":[{"name":"pattern","type":"string"}],"returns":"objid"},"write:text:":{"arguments":[{"name":"path","type":"string"},{"name":"text","type":"string"}],"returns":"string"},"grep:in:":{"arguments":[{"name":"text","type":"string"},{"name":"path","type":"string"}],"returns":"objid"},"run:args:":{"arguments":[{"name":"program","type":"string"},{"name":"arguments","type":"objid"}],"returns":"objid"},"exec:args:":{"arguments":[{"name":"program","type":"string"},{"name":"arguments","type":"objid"}],"returns":"objid"},"env:put:":{"arguments":[{"name":"name","type":"string"},{"name":"value","type":"string"}],"returns":"objid"}}}' ]
    inspectorViews [ ^ '{"views":[{"id":"shell","title":"Shell session","body":{"kind":"column","children":[{"kind":"input","label":"Directory","getter":"pwd","setter":"cd:","type":"string"},{"kind":"row","children":[{"kind":"action","text":"List files","method":"ls"},{"kind":"action","text":"Show jobs","method":"jobs"},{"kind":"action","text":"Last process","method":"lastProcess"}]},{"kind":"code","label":"Try in Workspace","text":"self ls.\nself find: ''*.st''.\n(self run: ''go'' args: #(''version'')) stdout"}]}}]}' ]
]
