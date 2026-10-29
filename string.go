package main

import (
	"fmt"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// shellTokenize splits a shell-ish command line into tokens.
// Quoting rules:
//   - Whitespace (space, tab) separates tokens.
//   - "..." and '...' wrap a single token; the quotes are stripped.
//   - \X escapes the next character literally, both inside and outside
//     quotes; backslash at end-of-line is dropped.
//   - Adjacent quoted/unquoted segments concatenate: a"b"c is one token
//     "abc".
//
// Not a posix shell parser — there's no variable expansion, command
// substitution, or here-doc handling.  Good enough for the example
// servicetalk shell where "echo \"hello world\"" should be two
// tokens, not three.
func shellTokenize(s string) []string {
	var (
		toks  []string
		buf   strings.Builder
		inTok bool
		quote rune
	)
	flush := func() {
		if inTok {
			toks = append(toks, buf.String())
			buf.Reset()
			inTok = false
		}
	}
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		if c == '\\' && i+1 < len(runes) {
			buf.WriteRune(runes[i+1])
			inTok = true
			i++
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				buf.WriteRune(c)
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			inTok = true
			continue
		}
		if c == ' ' || c == '\t' {
			flush()
			continue
		}
		buf.WriteRune(c)
		inTok = true
	}
	flush()
	return toks
}

// callStringMethod dispatches a message sent to a string value.
// Strings are not processes; this runs locally.
func callStringMethod(obj *ObjectServer, s string, method string, args []Value) Value {
	if sel, rest, ok := performArgs(obj, method, args); ok {
		return callStringMethod(obj, s, sel, rest)
	}
	switch method {

	case "class":
		return StrVal("String")
	case "isNil":
		return False
	case "notNil":
		return True
	case "yourself":
		return StrVal(s)
	case "printString", "displayString":
		// Wrap in single quotes, escaping embedded single quotes as ''.
		return StrVal("'" + strings.ReplaceAll(s, "'", "''") + "'")

	case "signal":
		panic(signalError{msg: s})

	case "size":
		return NumVal(float64(utf8.RuneCountInString(s)))

	case "at:":
		if len(args) < 1 || args[0].Kind != "number" {
			return Nil
		}
		runes := []rune(s)
		i := int(args[0].Num) // 0-based
		if i < 0 || i >= len(runes) {
			return Nil
		}
		return StrVal(string(runes[i]))

	case "copyFrom:to:":
		// Half-open [start, end) — both 0-based.
		if len(args) < 2 || args[0].Kind != "number" || args[1].Kind != "number" {
			return Nil
		}
		runes := []rune(s)
		start := int(args[0].Num)
		end := int(args[1].Num)
		if start < 0 {
			start = 0
		}
		if end > len(runes) {
			end = len(runes)
		}
		if start >= end {
			return StrVal("")
		}
		return StrVal(string(runes[start:end]))

	case "asUppercase":
		return StrVal(strings.ToUpper(s))
	case "asLowercase":
		return StrVal(strings.ToLower(s))

	case "includes:":
		if len(args) < 1 {
			return False
		}
		return BoolVal(strings.Contains(s, args[0].String()))

	case "replaceAll:with:":
		if len(args) < 2 {
			return StrVal(s)
		}
		return StrVal(strings.ReplaceAll(s, args[0].String(), args[1].String()))

	case ",":
		if len(args) < 1 {
			return StrVal(s)
		}
		return StrVal(s + args[0].String())

	case "=":
		if len(args) < 1 {
			return False
		}
		return BoolVal(s == args[0].String())

	case "~=":
		if len(args) < 1 {
			return True
		}
		return BoolVal(s != args[0].String())

	case "isEmpty":
		return BoolVal(s == "")

	case "notEmpty":
		return BoolVal(s != "")

	case "trimSeparators":
		return StrVal(strings.TrimSpace(s))

	case "startsWith:":
		if len(args) < 1 {
			return False
		}
		return BoolVal(strings.HasPrefix(s, args[0].String()))

	case "endsWith:":
		if len(args) < 1 {
			return False
		}
		return BoolVal(strings.HasSuffix(s, args[0].String()))

	case "indexOf:":
		if len(args) < 1 {
			return NumVal(-1)
		}
		needle := args[0].String()
		if needle == "" {
			return NumVal(-1)
		}
		// 0-based rune offset; returns -1 when the needle isn't found.
		idx := strings.Index(s, needle)
		if idx < 0 {
			return NumVal(-1)
		}
		return NumVal(float64(utf8.RuneCountInString(s[:idx])))

	case "shellTokens":
		// Splits the receiver as a shell-ish command line: whitespace
		// separates tokens, but `"..."` and `'...'` are taken as a
		// single token with the quotes stripped, and `\` escapes the
		// next char (always — inside or outside quotes).  Used by the
		// example servicetalk shell so `echo "hello world"` parses as
		// two tokens instead of three.  Not posix-faithful (no
		// variable substitution, no backtick subshells, no nested
		// quoting subtleties); good enough for casual use.
		if obj == nil {
			return Nil
		}
		toks := shellTokenize(s)
		arrID := obj.spawnBuiltin(BuiltinClassArray)
		if arrID == "" {
			return Nil
		}
		ref := ObjVal(arrID)
		for _, t := range toks {
			obj.sendMessage(ref, "add:", []Value{StrVal(t)})
		}
		return ref

	case "getenv":
		// Return the value of the OS environment variable named by the
		// receiver string, or nil if unset.  Used by the shell example
		// to expand `$VAR` references in built-in command args.
		if v, ok := os.LookupEnv(s); ok {
			return StrVal(v)
		}
		return Nil

	case "splitOn:":
		if len(args) < 1 || obj == nil {
			return Nil
		}
		sep := args[0].String()
		var parts []string
		if sep == "" {
			parts = []string{s}
		} else {
			parts = strings.Split(s, sep)
		}
		arrID := obj.spawnBuiltin(BuiltinClassArray)
		if arrID == "" {
			return Nil
		}
		arrRef := ObjVal(arrID)
		for _, p := range parts {
			obj.sendMessage(arrRef, "add:", []Value{StrVal(p)})
		}
		return arrRef

	case "urlHost":
		u, err := neturl.Parse(s)
		if err != nil || u == nil {
			return Nil
		}
		return StrVal(u.Host)

	case "urlPath":
		u, err := neturl.Parse(s)
		if err != nil || u == nil {
			return Nil
		}
		if u.Path == "" {
			return StrVal("/")
		}
		return StrVal(u.Path)

	case "urlScheme":
		u, err := neturl.Parse(s)
		if err != nil || u == nil {
			return Nil
		}
		return StrVal(u.Scheme)

	// ---- Filesystem helpers ----
	//
	// The receiver string is treated as a path.  These exist so user
	// code (e.g. the example shell) doesn't need a separate FileSystem
	// class for the basic stat / list / read operations.  Errors come
	// back as nil — the caller can probe with `fileExists` first if it
	// wants to distinguish "missing" from "unreadable".

	case "fileExists":
		_, err := os.Stat(s)
		return BoolVal(err == nil)

	case "isDirectory":
		fi, err := os.Stat(s)
		return BoolVal(err == nil && fi.IsDir())

	case "isFile":
		fi, err := os.Stat(s)
		return BoolVal(err == nil && fi.Mode().IsRegular())

	case "fileSize":
		fi, err := os.Stat(s)
		if err != nil {
			return Nil
		}
		return NumVal(float64(fi.Size()))

	case "readText":
		data, err := os.ReadFile(s)
		if err != nil {
			return Nil
		}
		return StrVal(string(data))

	case "dirEntries":
		// Returns an Array of basenames.  Empty Array on error / non-dir.
		// Sorted because every shell user expects deterministic order.
		entries, err := os.ReadDir(s)
		if err != nil || obj == nil {
			arrID := ""
			if obj != nil {
				arrID = obj.spawnBuiltin(BuiltinClassArray)
			}
			if arrID == "" {
				return Nil
			}
			return ObjVal(arrID)
		}
		arrID := obj.spawnBuiltin(BuiltinClassArray)
		if arrID == "" {
			return Nil
		}
		ref := ObjVal(arrID)
		for _, e := range entries {
			obj.sendMessage(ref, "add:", []Value{StrVal(e.Name())})
		}
		return ref

	case "absolutePath":
		abs, err := filepath.Abs(s)
		if err != nil {
			return StrVal(s)
		}
		return StrVal(abs)

	case "joinPath:":
		if len(args) < 1 {
			return StrVal(s)
		}
		return StrVal(filepath.Join(s, args[0].String()))

	case "basename":
		return StrVal(filepath.Base(s))

	case "dirname":
		return StrVal(filepath.Dir(s))

	case "lines":
		// Convenience: split on \n and drop a trailing empty piece if
		// the input ended with a newline.  Handles \r\n by stripping
		// the trailing \r per line so robots.txt files that came over
		// the wire CRLF-encoded parse the same as LF-encoded ones.
		if obj == nil {
			return Nil
		}
		raw := strings.Split(s, "\n")
		if len(raw) > 0 && raw[len(raw)-1] == "" {
			raw = raw[:len(raw)-1]
		}
		arrID := obj.spawnBuiltin(BuiltinClassArray)
		if arrID == "" {
			return Nil
		}
		arrRef := ObjVal(arrID)
		for _, line := range raw {
			line = strings.TrimRight(line, "\r")
			obj.sendMessage(arrRef, "add:", []Value{StrVal(line)})
		}
		return arrRef

	case "asInteger", "asNumber":
		var n float64
		if _, err := fmt.Sscanf(s, "%g", &n); err == nil {
			return NumVal(n)
		}
		return Nil

	case "asCharacter":
		// First rune as a Character.  Bridges `(s at: i) asCharacter` since
		// String at: returns a single-rune string, not a Character.
		for _, r := range s { // ranges by rune; first iteration is rune 0
			return CharVal(r)
		}
		return Nil // empty string

	case "reversed", "reverse":
		// Rune-aware reversal, so multi-byte characters survive intact.
		runes := []rune(s)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		return StrVal(string(runes))

	case "capitalize":
		// Upper-case the first rune, leave the rest untouched.
		if s == "" {
			return StrVal("")
		}
		runes := []rune(s)
		runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		return StrVal(string(runes))

	case "asArray", "chars":
		// Array of single-character strings, one per rune.
		if obj == nil {
			return Nil
		}
		arrID := obj.spawnBuiltin(BuiltinClassArray)
		if arrID == "" {
			return Nil
		}
		arrRef := ObjVal(arrID)
		for _, r := range s {
			obj.sendMessage(arrRef, "add:", []Value{StrVal(string(r))})
		}
		return arrRef

	case "repeat:":
		// Repeat the string N times: 'ab' repeat: 3 → 'ababab'
		if len(args) < 1 || args[0].Kind != "number" {
			return StrVal(s)
		}
		n := int(args[0].Num)
		if n < 0 {
			n = 0
		}
		return StrVal(strings.Repeat(s, n))

	case "count:":
		// Count non-overlapping occurrences of the argument substring.
		if len(args) < 1 {
			return NumVal(0)
		}
		return NumVal(float64(strings.Count(s, args[0].String())))

	case "padLeft:with:":
		// Left-pad to the given width using the pad character/string.
		if len(args) < 2 || args[0].Kind != "number" {
			return StrVal(s)
		}
		width := int(args[0].Num)
		pad := args[1].String()
		if pad == "" {
			pad = " "
		}
		// Use only the first rune of the pad string.
		padRune := []rune(pad)[0]
		runes := []rune(s)
		for len(runes) < width {
			runes = append([]rune{padRune}, runes...)
		}
		return StrVal(string(runes))

	case "padRight:with:":
		// Right-pad to the given width using the pad character/string.
		if len(args) < 2 || args[0].Kind != "number" {
			return StrVal(s)
		}
		width := int(args[0].Num)
		pad := args[1].String()
		if pad == "" {
			pad = " "
		}
		padRune := []rune(pad)[0]
		runes := []rune(s)
		for len(runes) < width {
			runes = append(runes, padRune)
		}
		return StrVal(string(runes))
	}

	return unsupportedMessage(StrVal(s), "String", method, args)
}
