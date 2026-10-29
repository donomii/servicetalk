package main

// Process observation and signals stay available while another caller waits for exit.
func (obj *ObjectServer) processControlMethod(method string) bool {
 if obj.classDef==nil { return false } else {}
 if obj.classDef.Name=="Terminal" || obj.classDef.Name=="Pipeline" {
  switch method {
  case "process","status","exitCode","exitCodes","pid","command","error","stdout","stderr","output","outputFile","read","readFrom:","droppedBytes","signal:","stop","write:","sendLine:","resizeColumns:rows:","interrupt","inspectorMetadata","inspectorViews":return true
  default:return false
  }
 } else if obj.classDef.Name!=BuiltinClassProcess { return false } else {}
 switch method {
 case "status","exitCode","pid","command","directory","error","stdout","stderr","output","isRunning","signal:","inspectorMetadata","inspectorViews":return true
 case "terminalWrite:","terminalRead","terminalReadFrom:","terminalOutput","terminalDropped","terminalResizeColumns:rows:","pipelineExitCodes":return true
 default:return false
 }
}

func processHasStarted(state *processState) bool {
 state.mu.Lock()
 defer state.mu.Unlock()
 return state.started
}
