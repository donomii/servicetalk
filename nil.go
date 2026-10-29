package main

// callNilMethod dispatches a message sent to the literal nil value.
// nil is not a process, so this runs locally — same shape as the
// number / string / bool dispatchers.
//
// Treating nil as an object that can receive messages closes the
// hole where `(send foo isNil)` would log
// `target is not an object ID: nil` whenever foo happened to be nil
// — which is exactly when you wanted to test it.
func callNilMethod(obj *ObjectServer, method string, args []Value) Value {
	if sel, rest, ok := performArgs(obj, method, args); ok {
		return callNilMethod(obj, sel, rest)
	}
	switch method {
	case "isNil":
		return True
	case "notNil":
		return False
	case "class":
		return StrVal("Nil")
	case "yourself":
		return Nil
	case "printString", "displayString":
		return StrVal("nil")

	// Identity — nil equals nil, nothing else.
	case "=":
		if len(args) < 1 {
			return False
		}
		return BoolVal(args[0].Kind == "nil" || args[0].Kind == "")
	case "~=", "!=":
		if len(args) < 1 {
			return True
		}
		return BoolVal(!(args[0].Kind == "nil" || args[0].Kind == ""))

	// Smalltalk-y conditionals so you don't have to nest sends.
	case "ifNil:":
		if len(args) > 0 && obj != nil {
			return obj.sendMessage(args[0], "value", nil)
		}
		return Nil
	case "ifNotNil:":
		return Nil
	case "ifNil:ifNotNil:":
		if len(args) >= 1 && obj != nil {
			return obj.sendMessage(args[0], "value", nil)
		}
		return Nil

	case "respondsTo:":
		if len(args) < 1 {
			return False
		}
		switch args[0].String() {
		case "isNil", "notNil", "class", "yourself",
			"printString", "displayString", "=", "~=", "!=",
			"ifNil:", "ifNotNil:", "ifNil:ifNotNil:":
			return True
		}
		return False
	}

	return unsupportedMessage(Nil, "Nil", method, args)
}
