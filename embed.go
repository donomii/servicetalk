package main

import "io/fs"

// embeddedClasses, when non-nil, is a filesystem of baked-in class source
// (ClassName.smt / ClassName.st) produced by `smt build`.  A normal build
// leaves it nil and all class loading goes to disk via classPath.  `smt
// build` writes a generated file into a throwaway source copy that sets this
// in init() to an embed.FS of the target app's classes, so the resulting
// binary is self-contained.
var embeddedClasses fs.FS

// embeddedClassSource returns the bytes of a baked-in class file (e.g.
// "Counter.smt") when running as a `smt build` single-binary, else ok=false.
func embeddedClassSource(name string) ([]byte, bool) {
	if embeddedClasses == nil {
		return nil, false
	}
	data, err := fs.ReadFile(embeddedClasses, name)
	if err != nil {
		return nil, false
	}
	return data, true
}
