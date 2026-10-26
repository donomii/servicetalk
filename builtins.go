package main

// Built-in class names — both forms in one place so a typo can't
// silently desync the user-facing alias from the internal class
// name the runtime dispatches against.
//
// Two names per built-in:
//
//   • the "user" name that appears in `(new Foo)` and the
//     coordinator/worker spawn aliases — what user .smt code sees;
//   • the "internal" name (with leading/trailing __) that the
//     runtime stamps on classDef.Name and that handleInspect /
//     callMethod / encodeJSON dispatch on.
//
// `coordinator.spawnLocal` and `worker.spawnLocal` rewrite the user
// name to the internal name via builtinSpawnAlias() so the spawned
// object subprocess is told the internal form.  evalNew (REPL /
// `(new …)`) does NOT rewrite — it stores the original (user) name
// in the local registry — which is why JSON encoding has to accept
// either form when matching against entry.ClassName.
const (
	UserClassArray         = "Array"
	UserClassDictionary    = "Dictionary"
	UserClassHttpClient    = "HttpClient"
	UserClassHttpServer    = "HttpServer"
	UserClassRobotsMatcher = "RobotsMatcher"
	UserClassSitemapParser = "SitemapParser"
	UserClassProcess       = "Process"

	BuiltinClassArray         = "__array__"
	BuiltinClassDictionary    = "__dictionary__"
	BuiltinClassHttpClient    = "__httpclient__"
	BuiltinClassHttpServer    = "__httpserver__"
	BuiltinClassRobotsMatcher = "__robotsmatcher__"
	BuiltinClassSitemapParser = "__sitemapparser__"
	BuiltinClassProcess       = "__process__"

	// Internal-only classes (no user-facing alias).
	BuiltinClassBlock       = "__block__"
	BuiltinClassWorkspace   = "__workspace__"
	BuiltinClassCoordinator = "__coordinator__"
)

// userToBuiltinClass maps the user-facing class name to the internal
// class name the runtime dispatches against.  Returns the input
// unchanged for non-builtin classes (so user classes pass through).
// Used by every entry point that turns a `(new Foo)` request into
// a spawn — coordinator.spawnLocal, worker.spawnLocal, callers of
// spawnObject().
func userToBuiltinClass(name string) string {
	switch name {
	case UserClassArray:
		return BuiltinClassArray
	case UserClassDictionary:
		return BuiltinClassDictionary
	case UserClassHttpClient:
		return BuiltinClassHttpClient
	case UserClassHttpServer:
		return BuiltinClassHttpServer
	case UserClassRobotsMatcher:
		return BuiltinClassRobotsMatcher
	case UserClassSitemapParser:
		return BuiltinClassSitemapParser
	case UserClassProcess:
		return BuiltinClassProcess
	}
	return name
}

// isArrayClass / isDictionaryClass — a builtin instance can be
// labelled with either the user or the internal name depending on
// which spawn path created it (see comment on the constants above).
// These predicates centralise the both-forms check so JSON encoding
// and the like don't have to enumerate the pairs themselves.
func isArrayClass(name string) bool {
	return name == BuiltinClassArray || name == UserClassArray
}

func isDictionaryClass(name string) bool {
	return name == BuiltinClassDictionary || name == UserClassDictionary
}
