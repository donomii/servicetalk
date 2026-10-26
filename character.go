package main

import "unicode"

// callCharacterMethod dispatches a message sent to a Character value.
// Characters are primitives (like numbers and booleans), not processes:
// this runs locally with no spawning.  A Character holds one Unicode code
// point (rune), so every predicate and conversion here is correct for the
// full Unicode range, not just ASCII/Latin-1/BMP.
//
// Construct a Character with the `$x` literal, `n asCharacter` (code point →
// Character), or `aString asCharacter` (first rune).  There is no
// `Character value:` class method — the runtime has no class objects (see
// TODO), so `asCharacter` is the constructor.
func callCharacterMethod(obj *ObjectServer, r rune, method string, args []Value) Value {
	if sel, rest, ok := performArgs(obj, method, args); ok {
		return callCharacterMethod(obj, r, sel, rest)
	}
	switch method {

	// ---- identity / type ----
	case "class":
		return StrVal("Character")
	case "isNil":
		return False
	case "notNil":
		return True
	case "yourself":
		return CharVal(r)
	case "printString", "displayString":
		// Smalltalk literal form: $a (and $  for space).
		return StrVal("$" + string(r))
	case "inspect":
		obj.transcriptf("$%c", r)
		return CharVal(r)

	// ---- conversions ----
	case "asString", "asCharacter":
		if method == "asCharacter" {
			return CharVal(r)
		}
		return StrVal(string(r))
	case "value", "asInteger", "codePoint", "asNumber":
		return NumVal(float64(r))
	case "asUppercase":
		return CharVal(unicode.ToUpper(r))
	case "asLowercase":
		return CharVal(unicode.ToLower(r))

	// ---- comparison (by code point) ----
	case "=":
		return BoolVal(len(args) >= 1 && args[0].Kind == "character" && args[0].Char == r)
	case "~=", "!=":
		return BoolVal(!(len(args) >= 1 && args[0].Kind == "character" && args[0].Char == r))
	case "<", ">", "<=", ">=":
		if len(args) < 1 || args[0].Kind != "character" {
			return Nil
		}
		o := args[0].Char
		switch method {
		case "<":
			return BoolVal(r < o)
		case ">":
			return BoolVal(r > o)
		case "<=":
			return BoolVal(r <= o)
		default:
			return BoolVal(r >= o)
		}

	// ---- Unicode predicates ----
	case "isLetter":
		return BoolVal(unicode.IsLetter(r))
	case "isDigit":
		return BoolVal(unicode.IsDigit(r))
	case "isLetterOrDigit", "isAlphaNumeric":
		return BoolVal(unicode.IsLetter(r) || unicode.IsDigit(r))
	case "isUppercase":
		return BoolVal(unicode.IsUpper(r))
	case "isLowercase":
		return BoolVal(unicode.IsLower(r))
	case "isSpace", "isSeparator":
		return BoolVal(unicode.IsSpace(r))
	case "isPunctuation":
		return BoolVal(unicode.IsPunct(r))
	}

	if obj != nil {
		obj.transcriptf("Character doesNotUnderstand: %s", method)
	}
	return Nil
}
