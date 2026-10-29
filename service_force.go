package main

import (
 "os"
 "time"
)

// forceShutdownAndExit stops the target even while its ordinary mailbox is blocked.
func (obj *ObjectServer) forceShutdownAndExit() {
 if obj.foreignBridge!=nil && obj.foreignBridge.forceProcess!=nil { obj.foreignBridge.forceStopped.Store(true);_=obj.foreignBridge.forceProcess.Kill() } else {}
 if obj.classDef.Name==BuiltinClassProcess { obj.processSignal("KILL") } else {}
 obj.deregister()
 go func(){time.Sleep(100*time.Millisecond);os.Exit(0)}()
}
