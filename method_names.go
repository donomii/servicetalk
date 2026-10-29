package main

func (obj *ObjectServer) inspectionMethodNames() []string {
	var methods []string
	switch obj.classDef.Name {
	case BuiltinClassWorkspace:
		methods = []string{"eval:", "evalScript:", "evalResult:"}
	case BuiltinClassArray:
		methods = []string{"add:", "at:", "at:put:", "size", "first", "last", "includes:", "do:", "collect:", "parallelSend:withEach:", "parallelSend:withEach:timeoutMs:", "printAll", "printString", "isEmpty", "notEmpty", "indexOf:", "remove:", "removeAt:", "sort", "sort:", "reverse", "reversed", "copy", "asArray", "addAll:", "select:", "reject:", "detect:", "detect:ifNone:", "inject:into:", "with:do:", "max", "min", "join:", "any:", "all:"}
	case BuiltinClassDictionary:
		methods = []string{"at:", "at:put:", "at:ifAbsent:", "includesKey:", "removeKey:", "do:", "keysAndValuesDo:", "keys", "values", "size", "printAll", "printString", "isEmpty", "notEmpty", "at:ifAbsentPut:", "copy", "collect:", "select:", "reject:", "at:ifPresent:"}
	case BuiltinClassHttpClient:
		methods = []string{"get:", "get:timeoutMs:", "get:headers:", "post:body:", "post:body:contentType:"}
	case BuiltinClassHttpServer:
		methods = []string{"listenOn:handler:", "listenOn:handler:methodName:", "stop", "isRunning", "port"}
	case BuiltinClassRobotsMatcher:
		methods = []string{"parse:", "allows:agent:", "crawlDelayFor:", "agentCount"}
	case BuiltinClassSitemapParser:
		methods = []string{"parse:", "pageUrls", "indexUrls", "empty"}
	case BuiltinClassProcess:
		methods = []string{"start:", "start:cwd:", "start:args:", "start:args:cwd:", "input:", "environment:", "output", "outputLines", "stdout", "stderr", "error", "directory",
			"terminalStart:args:cwd:columns:rows:", "terminalWrite:", "terminalRead", "terminalReadFrom:", "terminalOutput", "terminalDropped", "terminalResizeColumns:rows:", "pipeline:cwd:outputFile:", "pipelineExitCodes",
			"status", "exitCode", "pid", "command", "signal:", "wait", "isRunning", "inspectorMetadata", "inspectorViews"}
	case BuiltinClassBlock:
		methods = []string{"value", "value:", "value:value:", "free"}
	default:
		if obj.classDef != nil {
			for name := range inspectorSignatures(obj.classDef) {
				methods = append(methods, name)
			}
		}
	}

	return methods
}
