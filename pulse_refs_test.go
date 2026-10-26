package main

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestEscapedBlockKeepsLexicalOwnerReachable(t *testing.T) {
	obj := newTestObject(BuiltinClassBlock)
	obj.blockDef = &BlockDef{
		ParentID:     "lexical-owner",
		CapturedArgs: map[string]Value{"dependency": ObjVal("captured-object")},
	}
	seen := map[string]bool{}
	for _, id := range obj.collectObjidRefs() {
		seen[id] = true
	}
	for _, id := range []string{"lexical-owner", "captured-object"} {
		if !seen[id] {
			t.Fatalf("live closure must retain %s; reachability references: %v", id, seen)
		}
	}
	obj.blockDef.ParentID = ""
	for _, id := range obj.collectObjidRefs() {
		if id == "" {
			t.Fatal("empty lexical parent must not become a pulse target")
		}
	}
}

func TestBlockPulseReachesLexicalOwnerThroughNATS(t *testing.T) {
	withNATSTestServer(t, func() {
		owner := newTestObject("Owner")
		owner.id = "lexical-owner"
		block := newTestObject(BuiltinClassBlock)
		block.id = "escaped-block"
		block.blockDef = &BlockDef{ParentID: owner.id}
		block.registry.Store(owner.id, ObjectEntry{ID: owner.id, Subject: natsSubjectForObject(owner.id)})
		if err := owner.startNATSReceiver(); err != nil {
			t.Fatal(err)
		}
		if err := block.startNATSReceiver(); err != nil {
			t.Fatal(err)
		}
		sweep := time.Now().UnixMilli()
		if _, err := sendNATSCall(natsSubjectForObject(block.id), "pulse:", []Value{NumVal(float64(sweep))}, time.Second); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if atomic.LoadInt64(&owner.lastSweepID) == sweep {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("a live escaped block did not pulse its lexical owner")
	})
}
