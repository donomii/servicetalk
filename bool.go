package main

// callBoolMethod dispatches a message sent to a boolean value.
func callBoolMethod(obj *ObjectServer, b bool, method string, args []Value) Value {
	if sel, rest, ok := performArgs(obj, method, args); ok {
		return callBoolMethod(obj, b, sel, rest)
	}
	switch method {
	case "not":
		return BoolVal(!b)
	case "and:":
		if len(args) < 1 {
			return False
		}
		if !b {
			return False
		}
		if args[0].Kind == "objid" && obj != nil {
			return BoolVal(obj.sendMessage(args[0], "value", nil).Truthy())
		}
		return BoolVal(args[0].Truthy())
	case "or:":
		if len(args) < 1 {
			return BoolVal(b)
		}
		if b {
			return True
		}
		if args[0].Kind == "objid" && obj != nil {
			return BoolVal(obj.sendMessage(args[0], "value", nil).Truthy())
		}
		return BoolVal(args[0].Truthy())
	case "xor:":
		if len(args) < 1 {
			return BoolVal(b)
		}
		return BoolVal(b != args[0].Truthy())
	case "ifTrue:":
		if b && len(args) > 0 && obj != nil {
			return obj.sendMessage(args[0], "value", nil)
		}
		return Nil
	case "ifFalse:":
		if !b && len(args) > 0 && obj != nil {
			return obj.sendMessage(args[0], "value", nil)
		}
		return Nil
	case "ifTrue:ifFalse:":
		if len(args) < 2 || obj == nil {
			return Nil
		}
		if b {
			return obj.sendMessage(args[0], "value", nil)
		}
		return obj.sendMessage(args[1], "value", nil)
	case "=":
		if len(args) < 1 {
			return False
		}
		return BoolVal(b == args[0].Truthy())
	case "printString", "displayString":
		if b {
			return StrVal("true")
		}
		return StrVal("false")
	case "class":
		return StrVal("Boolean")
	case "isNil":
		return False
	case "notNil":
		return True
	case "yourself":
		return BoolVal(b)
	}
	return Nil
}
