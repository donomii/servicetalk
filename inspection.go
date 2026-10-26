package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync/atomic"
	"time"
)

func (obj *ObjectServer) trackCall(id, chain, method, target, state string) func() {
	obj.setCallActivity(id, CallActivity{ID: id, ChainID: chain, Method: method, Target: target, State: state, Started: time.Now()})
	return func() { obj.removeCallActivity(id) }
}

func (obj *ObjectServer) setCallActivity(id string, call CallActivity) {
	obj.activityMu.Lock()
	defer obj.activityMu.Unlock()
	if obj.activity == nil {
		obj.activity = make(map[string]CallActivity)
	} else {
		// Retain other active and reentrant calls.
	}
	obj.activity[id] = call
}

func (obj *ObjectServer) removeCallActivity(id string) {
	obj.activityMu.Lock()
	defer obj.activityMu.Unlock()
	delete(obj.activity, id)
}

func (obj *ObjectServer) callActivitySnapshot() []CallActivity {
	obj.activityMu.Lock()
	defer obj.activityMu.Unlock()
	calls := make([]CallActivity, 0, len(obj.activity))
	for _, call := range obj.activity {
		calls = append(calls, call)
	}
	return calls
}

func (obj *ObjectServer) runtimeInspection() *RuntimeInspection {
	host, _ := os.Hostname()
	result := &RuntimeInspection{Host: host, PID: os.Getpid(), WorkerID: os.Getenv("SMT_WORKER_ID"), Calls: []CallActivity{}, References: obj.collectObjidRefs(), ForeignReferences: obj.foreignBridge != nil, PulseTimeoutMs: obj.pulseTimeout.Milliseconds(), Metrics: obj.metricsResponse()}
	result.Calls = obj.callActivitySnapshot()
	result.Prototype = obj.prototypeID
	result.Promise = obj.promiseSnapshot()
	if cloneableClass(obj.classDef) && obj.foreignBridge == nil { result.DefinitionVersion = definitionVersion(snapshotDefinition(obj.classDef)) } else {}
	if session := obj.debugSession(); session != nil { snapshot := session.snapshot(); result.Debug = &snapshot } else {}
	result.LastSweepID = atomic.LoadInt64(&obj.lastSweepID)
	result.PulseAgeMs = obj.pulseAge(time.Now()).Milliseconds()
	sort.Slice(result.Calls, func(i, j int) bool { return result.Calls[i].Started.Before(result.Calls[j].Started) })
	sort.Strings(result.References)
	return result
}

func (c *Coordinator) inspectObject(id string, retention bool) (InspectResponse, error) {
	if id == coordinatorID {
		host, _ := os.Hostname()
		return InspectResponse{ObjectID: id, ClassName: BuiltinClassCoordinator, Methods: []string{"allObjects", "objectsOfClass:", "pin:", "unpin:", "isPinned:"}, InstanceVars: map[string]Value{"objects": NumVal(float64(len(c.objectSnapshot())))}, Runtime: &RuntimeInspection{Host: host, PID: os.Getpid(), Calls: []CallActivity{}, References: c.rootSnapshot()}, Retention: &RetentionInspection{Note: "The coordinator owns the registry and GC root set."}}, nil
	} else {
		// Object state is read through the control mailbox while normal work runs.
	}
	reply, err := c.callObjectID(id, "__inspectJson", nil, inspectionTimeout)
	var result InspectResponse
	if err != nil {
		return result, err
	} else if err := json.Unmarshal([]byte(reply.Str), &result); err != nil {
		return result, fmt.Errorf("inspect %s: decode object reply: %w", id, err)
	} else if retention {
		result.Retention = c.explainRetention(id)
		return result, nil
	} else {
		return result, nil
	}
}

func (c *Coordinator) rootSnapshot() []string {
	c.rootsMu.Lock()
	defer c.rootsMu.Unlock()
	ids := make([]string, 0, len(c.roots))
	for id := range c.roots {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (c *Coordinator) explainRetention(id string) *RetentionInspection {
	result := &RetentionInspection{Path: []string{}, Unavailable: []string{}, Note: "Sampled runtime-visible references; foreign state and references in transit may be absent. No path is not proof of unreachability."}
	paths := map[string][]string{}
	queue := c.rootSnapshot()
	for _, root := range queue {
		paths[root] = []string{root}
	}
	_, result.Root = paths[id]
	deadline := time.Now().Add(inspectionTimeout)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == id {
			result.Path = paths[current]
			return result
		} else if time.Now().After(deadline) {
			result.Unavailable = append(result.Unavailable, current)
			result.Unavailable = append(result.Unavailable, queue...)
			return result
		} else if item, err := c.inspectObject(current, false); err != nil || item.Runtime == nil {
			result.Unavailable = append(result.Unavailable, current)
		} else {
			for _, ref := range item.Runtime.References {
				if _, seen := paths[ref]; seen {
					continue
				} else {
					paths[ref] = append(append([]string{}, paths[current]...), ref)
					queue = append(queue, ref)
				}
			}
		}
	}
	return result
}
