package main

import (
	"encoding/json"
	"strconv"
	"strings"
)

// encodeJSON renders a Smalltalk Value as a JSON string by walking
// Array and Dictionary objects via sendMessage.  Called from the
// `(jsonEncode val)` special form; safe to call recursively.
//
// The seen map breaks cycles: if the same object ID shows up while
// the encoder is still inside its own subtree, we emit "null" rather
// than recursing forever.  Cycles are theoretically possible because
// Dictionary values can include any Value, including the dict itself.
//
// Limitations to remember:
//
//   - Only Array and Dictionary objids are walked.  User classes
//     return "null" unless they expose their state through one of
//     those types and the caller does the conversion.
//   - Numbers render as int when integral, otherwise via strconv's
//     "g" format.  Special values (NaN, Inf) render as "null" so
//     the output stays valid JSON.
//   - Dictionary keys are always rendered as JSON strings.  Numeric
//     keys are first stringified — JSON requires string keys anyway.
func encodeJSON(obj *ObjectServer, v Value, seen map[string]bool) string {
	switch v.Kind {
	case "", "nil":
		return "null"
	case "bool":
		if v.Bool {
			return "true"
		}
		return "false"
	case "number":
		return encodeJSONNumber(v.Num)
	case "string":
		return encodeJSONString(v.Str)
	case "objid":
		if obj == nil {
			return "null"
		}
		if seen[v.ObjID] {
			return "null"
		}
		seen[v.ObjID] = true
		defer delete(seen, v.ObjID)

		entry, ok := obj.lookupRegistry(v.ObjID)
		if !ok {
			return "null"
		}
		switch {
		case isArrayClass(entry.ClassName):
			return encodeJSONArray(obj, v, seen)
		case isDictionaryClass(entry.ClassName):
			return encodeJSONDict(obj, v, seen)
		default:
			return "null"
		}
	}
	return "null"
}

func encodeJSONArray(obj *ObjectServer, arr Value, seen map[string]bool) string {
	sz := obj.sendMessage(arr, "size", nil)
	n := 0
	if sz.Kind == "number" {
		n = int(sz.Num)
	}
	parts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		el := obj.sendMessage(arr, "at:", []Value{NumVal(float64(i))})
		parts = append(parts, encodeJSON(obj, el, seen))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func encodeJSONDict(obj *ObjectServer, dict Value, seen map[string]bool) string {
	keys := obj.sendMessage(dict, "keys", nil)
	if keys.Kind != "objid" {
		return "{}"
	}
	sz := obj.sendMessage(keys, "size", nil)
	n := 0
	if sz.Kind == "number" {
		n = int(sz.Num)
	}
	parts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		k := obj.sendMessage(keys, "at:", []Value{NumVal(float64(i))})
		val := obj.sendMessage(dict, "at:", []Value{k})
		parts = append(parts, encodeJSONString(k.String())+":"+encodeJSON(obj, val, seen))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func encodeJSONString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// decodeJSONString parses a JSON document and materialises it as a
// Value tree.  Maps and arrays spawn fresh Dictionary / Array
// objects; primitive types use their direct Value kinds.
func decodeJSONString(obj *ObjectServer, s string) Value {
	var raw interface{}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return Nil
	}
	return decodeJSONValue(obj, raw)
}

func decodeJSONValue(obj *ObjectServer, raw interface{}) Value {
	switch v := raw.(type) {
	case nil:
		return Nil
	case bool:
		return BoolVal(v)
	case json.Number:
		// Prefer integer parse first so whole numbers don't pick up
		// a floating-point representation.
		if i, err := v.Int64(); err == nil {
			return NumVal(float64(i))
		}
		if f, err := v.Float64(); err == nil {
			return NumVal(f)
		}
		return NumVal(0)
	case float64:
		return NumVal(v)
	case string:
		return StrVal(v)
	case []interface{}:
		if obj == nil {
			return Nil
		}
		arrID := obj.spawnBuiltin(BuiltinClassArray)
		if arrID == "" {
			return Nil
		}
		arrRef := ObjVal(arrID)
		for _, el := range v {
			obj.sendMessage(arrRef, "add:", []Value{decodeJSONValue(obj, el)})
		}
		return arrRef
	case map[string]interface{}:
		if obj == nil {
			return Nil
		}
		dictID := obj.spawnBuiltin(BuiltinClassDictionary)
		if dictID == "" {
			return Nil
		}
		dictRef := ObjVal(dictID)
		for k, el := range v {
			obj.sendMessage(dictRef, "at:put:", []Value{StrVal(k), decodeJSONValue(obj, el)})
		}
		return dictRef
	}
	return Nil
}

func encodeJSONNumber(n float64) string {
	if n != n || n*0 != 0 {
		// NaN or +/-Inf — JSON has no representation; emit null.
		return "null"
	}
	if n == float64(int64(n)) {
		return strconv.FormatInt(int64(n), 10)
	}
	return strconv.FormatFloat(n, 'g', -1, 64)
}
