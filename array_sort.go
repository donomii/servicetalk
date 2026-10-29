package main

import (
	"fmt"
	"sort"
)

func (obj *ObjectServer) sortArrayWith(block Value) Value {
	original := obj.arraySortSnapshot()
	sorted := append([]Value(nil), original...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return obj.sendMessage(block, "value:value:", []Value{sorted[i], sorted[j]}).Truthy()
	})
	if obj.commitArraySort(original, sorted) {
		return ObjVal(obj.id)
	} else {
		panic(signalError{msg: fmt.Sprintf("Array %s changed while its sort: comparator ran; expected unchanged contents before applying the sorted result", obj.id)})
	}
}

func (obj *ObjectServer) arraySortSnapshot() []Value {
	obj.mu.RLock()
	defer obj.mu.RUnlock()
	return append([]Value(nil), obj.arrayData...)
}

func (obj *ObjectServer) commitArraySort(original, sorted []Value) bool {
	obj.mu.Lock()
	defer obj.mu.Unlock()
	if len(original) != len(obj.arrayData) {
		return false
	} else {
		for i, value := range original {
			if value != obj.arrayData[i] {
				return false
			} else {}
		}
		obj.arrayData = sorted
		return true
	}
}
