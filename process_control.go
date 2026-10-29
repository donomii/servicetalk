package main

// Process observation and signals stay available while another caller waits for exit.
func (obj *ObjectServer) processControlMethod(method string) bool {
 if obj.classDef==nil || obj.classDef.Name!=BuiltinClassProcess { return false } else {}
 switch method {
 case "status","exitCode","pid","command","directory","error","stdout","stderr","output","isRunning","signal:","inspectorMetadata","inspectorViews":return true
 default:return false
 }
}

func processHasStarted(state *processState) bool {
 state.mu.Lock()
 defer state.mu.Unlock()
 return state.started
}
