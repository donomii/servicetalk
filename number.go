package main

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
)

// ---- Number.smt lazy loader ----

var (
	numberClassOnce sync.Once
	numberClassDef  *ClassDef // nil if Number.smt not present
)

// loadNumberClass attempts to read Number.smt from the current directory once.
// Returns nil (silently) if the file doesn't exist.
func loadNumberClass() *ClassDef {
	numberClassOnce.Do(func() {
		cd, err := LoadClass("Number")
		if err == nil {
			numberClassDef = cd
		}
	})
	return numberClassDef
}

// ---- Number virtual dispatch ----

// callNumberMethod dispatches a message sent to a number value.
// Numbers are NOT processes; this runs locally with no spawning.
// If the method isn't built-in, Number.smt is consulted (if present).
func callNumberMethod(obj *ObjectServer, n float64, method string, args []Value) Value {
	if sel, rest, ok := performArgs(obj, method, args); ok {
		return callNumberMethod(obj, n, sel, rest)
	}
	switch method {

	// ---- identity / type ----
	case "class":
		return StrVal("Number")
	case "isNil":
		return False
	case "notNil":
		return True
	case "yourself":
		return NumVal(n)
	case "printString", "asString", "displayString":
		return StrVal(NumVal(n).String())
	case "asCharacter":
		// Code point → Character.  The constructor for characters, since
		// there is no Character class object (see TODO / character.go).
		return CharVal(rune(int64(n)))

	// ---- arithmetic ----
	case "+":
		if len(args) > 0 && args[0].Kind == "number" {
			return NumVal(n + args[0].Num)
		}
	case "-":
		if len(args) > 0 && args[0].Kind == "number" {
			return NumVal(n - args[0].Num)
		}
	case "*":
		if len(args) > 0 && args[0].Kind == "number" {
			return NumVal(n * args[0].Num)
		}
	case "/":
		if len(args) == 1 && args[0].Kind == "number" && args[0].Num != 0 {
			return NumVal(n / args[0].Num)
		} else {
			panic(signalError{msg: fmt.Sprintf("Number %v / requires one nonzero numeric divisor; received %v", n, args)})
		}
	case "//": // integer division
		if len(args) == 1 && args[0].Kind == "number" && args[0].Num != 0 {
			return NumVal(math.Floor(n / args[0].Num))
		} else {
			panic(signalError{msg: fmt.Sprintf("Number %v // requires one nonzero numeric divisor; received %v", n, args)})
		}
	case "\\\\":  // modulo
		if len(args) == 1 && args[0].Kind == "number" && args[0].Num != 0 {
			return NumVal(math.Mod(n, args[0].Num))
		} else {
			panic(signalError{msg: fmt.Sprintf("Number %v \\\\ requires one nonzero numeric divisor; received %v", n, args)})
		}
	case "**", "raisedTo:":
		if len(args) > 0 && args[0].Kind == "number" {
			return NumVal(math.Pow(n, args[0].Num))
		}

	// ---- unary math ----
	case "negated":
		return NumVal(-n)
	case "abs":
		return NumVal(math.Abs(n))
	case "sqrt":
		return NumVal(math.Sqrt(n))
	case "squared":
		return NumVal(n * n)
	case "reciprocal":
		if n != 0 {
			return NumVal(1.0 / n)
		} else {
			panic(signalError{msg: "Number 0 reciprocal requires a nonzero receiver"})
		}
	case "ceiling":
		return NumVal(math.Ceil(n))
	case "floor":
		return NumVal(math.Floor(n))
	case "rounded":
		return NumVal(math.Round(n))
	case "truncated":
		return NumVal(math.Trunc(n))
	case "ln":
		return NumVal(math.Log(n))
	case "log":
		return NumVal(math.Log10(n))
	case "exp":
		return NumVal(math.Exp(n))
	case "sin":
		return NumVal(math.Sin(n))
	case "cos":
		return NumVal(math.Cos(n))
	case "tan":
		return NumVal(math.Tan(n))

	// ---- integer operations ----
	case "factorial":
		if n < 0 || n > 170 || n != math.Trunc(n) {
			return Nil
		}
		r := 1.0
		for i := 2.0; i <= n; i++ {
			r *= i
		}
		return NumVal(r)

	case "isPrime":
		if n < 2 || n != math.Trunc(n) {
			return False
		}
		ni := int64(n)
		for i := int64(2); i*i <= ni; i++ {
			if ni%i == 0 {
				return False
			}
		}
		return True

	case "even":
		return BoolVal(int64(n)%2 == 0)
	case "odd":
		return BoolVal(int64(n)%2 != 0)

	case "gcd:":
		if len(args) < 1 || args[0].Kind != "number" {
			return Nil
		}
		a, b := int64(math.Abs(n)), int64(math.Abs(args[0].Num))
		for b != 0 {
			a, b = b, a%b
		}
		return NumVal(float64(a))

	case "lcm:":
		if len(args) < 1 || args[0].Kind != "number" {
			return Nil
		}
		a, b := int64(math.Abs(n)), int64(math.Abs(args[0].Num))
		if a == 0 || b == 0 {
			return NumVal(0)
		}
		g := a
		for tmp := b; tmp != 0; {
			g, tmp = tmp, g%tmp
		}
		return NumVal(float64(a / g * b))

	// ---- comparison ----
	case "=":
		if len(args) > 0 && args[0].Kind == "number" {
			return BoolVal(n == args[0].Num)
		}
		return False
	case "~=":
		if len(args) > 0 && args[0].Kind == "number" {
			return BoolVal(n != args[0].Num)
		}
		return True
	case "<":
		if len(args) > 0 && args[0].Kind == "number" {
			return BoolVal(n < args[0].Num)
		}
	case ">":
		if len(args) > 0 && args[0].Kind == "number" {
			return BoolVal(n > args[0].Num)
		}
	case "<=":
		if len(args) > 0 && args[0].Kind == "number" {
			return BoolVal(n <= args[0].Num)
		}
	case ">=":
		if len(args) > 0 && args[0].Kind == "number" {
			return BoolVal(n >= args[0].Num)
		}

	case "max:":
		if len(args) > 0 && args[0].Kind == "number" {
			return NumVal(math.Max(n, args[0].Num))
		}
	case "min:":
		if len(args) > 0 && args[0].Kind == "number" {
			return NumVal(math.Min(n, args[0].Num))
		}

	case "between:and:":
		if len(args) >= 2 && args[0].Kind == "number" && args[1].Kind == "number" {
			return BoolVal(n >= args[0].Num && n <= args[1].Num)
		}

	// ---- signal ----
	case "signal":
		panic(signalError{msg: NumVal(n).String()})

	// ---- sign / bitwise / random ----
	case "sign":
		if n > 0 {
			return NumVal(1)
		}
		if n < 0 {
			return NumVal(-1)
		}
		return NumVal(0)

	case "bitAnd:":
		if len(args) < 1 || args[0].Kind != "number" {
			return Nil
		}
		return NumVal(float64(int64(n) & int64(args[0].Num)))

	case "bitOr:":
		if len(args) < 1 || args[0].Kind != "number" {
			return Nil
		}
		return NumVal(float64(int64(n) | int64(args[0].Num)))

	case "bitXor:":
		if len(args) < 1 || args[0].Kind != "number" {
			return Nil
		}
		return NumVal(float64(int64(n) ^ int64(args[0].Num)))

	case "bitShift:":
		// Positive shift = left (<<), negative = right (>>).
		if len(args) < 1 || args[0].Kind != "number" {
			return Nil
		}
		i := int64(n)
		shift := int(args[0].Num)
		if shift >= 0 {
			return NumVal(float64(i << uint(shift)))
		}
		return NumVal(float64(i >> uint(-shift)))

	case "random":
		// Returns a uniformly distributed float in [0, 1).
		return NumVal(rand.Float64())

	// ---- iteration ----

	// (send n timesRepeat: aBlock) — call block n times
	case "timesRepeat:":
		if len(args) == 0 {
			return Nil
		}
		block := args[0]
		for i := 0; i < int(n); i++ {
			obj.sendMessage(block, "value", nil)
		}
		return Nil

	// (send start to: end) — returns Array [start .. end]
	case "to:":
		if len(args) == 0 || args[0].Kind != "number" {
			return Nil
		}
		end := args[0].Num
		arrID := obj.spawnBuiltin(BuiltinClassArray)
		if arrID == "" {
			return Nil
		}
		arr := ObjVal(arrID)
		for i := n; i <= end; i++ {
			obj.sendMessage(arr, "add:", []Value{NumVal(i)})
		}
		return arr

	// (send start to:do: end aBlock) — iterate, calling block value: i each step
	case "to:do:":
		if len(args) < 2 || args[0].Kind != "number" {
			return Nil
		}
		end, block := args[0].Num, args[1]
		for i := n; i <= end; i++ {
			obj.sendMessage(block, "value:", []Value{NumVal(i)})
		}
		return Nil

	// (send start to:by:do: end step aBlock)
	case "to:by:do:":
		if len(args) < 3 || args[0].Kind != "number" || args[1].Kind != "number" {
			return Nil
		}
		end, step, block := args[0].Num, args[1].Num, args[2]
		if step == 0 {
			return Nil
		}
		for i := n; (step > 0 && i <= end) || (step < 0 && i >= end); i += step {
			obj.sendMessage(block, "value:", []Value{NumVal(i)})
		}
		return Nil
	}

	// ---- user-defined Number.smt methods ----
	if cd := loadNumberClass(); cd != nil {
		if m, ok := cd.Methods[method]; ok {
			argMap := make(map[string]Value, len(m.Params))
			for i, p := range m.Params {
				if i < len(args) {
					argMap[p] = args[i]
				} else {
					argMap[p] = Nil
				}
			}
			selfVal := NumVal(n)
			ctx := &EvalCtx{obj: obj, args: argMap, selfOverride: &selfVal}
			return obj.runBody(ctx, m.Body)
		}
	}

	return unsupportedMessage(NumVal(n), "Number", method, args)
}
