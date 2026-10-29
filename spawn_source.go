package main

import (
 "fmt"
 "os"
 "strings"
)

func spawnSourceArguments(bundle *SourceBundle) ([]string,error) {
 if bundle==nil { return ClassPathArgs(),nil } else {}
 directory,err:=stageSourceBundle(bundle)
 if err!=nil { return nil,fmt.Errorf("stage source bundle %s: %w",bundle.Digest,err) } else if directory=="" { return nil,fmt.Errorf("source bundle requires a digest and source files") } else {}
 return append([]string{"--path",directory},ClassPathArgs()...),nil
}

func spawnSourceEnvironment(bundle *SourceBundle,paths []string) []string {
 environment:=os.Environ()
 if bundle==nil { return environment } else {}
 filtered:=make([]string,0,len(environment))
 for _,entry:=range environment { if strings.HasPrefix(entry,"SMT_PATH=") || strings.HasPrefix(entry,"SMT_SOURCE_BUNDLE_DIR=") { continue } else { filtered=append(filtered,entry) } }
 return append(filtered,"SMT_SOURCE_BUNDLE_DIR="+paths[1])
}
