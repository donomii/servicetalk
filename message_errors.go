package main

import "fmt"

func unsupportedMessage(receiver Value, className, selector string, args []Value) Value {
	panic(signalError{msg: fmt.Sprintf("%s %s does not understand %q with %d argument(s); expected a defined method", className, receiver.String(), selector, len(args))})
}
