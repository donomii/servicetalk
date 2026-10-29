package main

import (
 "bytes"
 "encoding/json"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "sort"
)

const sourceBundleManifestName = ".servicetalk-source-bundle.json"

// ensureSourceBundleManifest publishes the exact input snapshot, separate from foreign build products.
func ensureSourceBundleManifest(directory string,bundle *SourceBundle) error {
 data,err:=json.Marshal(bundle)
 if err!=nil { return err } else {}
 destination:=filepath.Join(directory,sourceBundleManifestName)
 existing,err:=readSourceManifestBounded(destination,int64(len(data))+1)
 if err==nil {
  if bytes.Equal(existing,data) { return nil } else { return fmt.Errorf("cached source bundle manifest differs from immutable version %s",bundle.Digest) }
 } else if !os.IsNotExist(err) { return err } else {}
 temporary,err:=os.CreateTemp(directory,".bundle-manifest-")
 if err!=nil { return err } else {}
 defer os.Remove(temporary.Name())
 _,writeErr:=temporary.Write(data);closeErr:=temporary.Close()
 if writeErr!=nil { return writeErr } else if closeErr!=nil { return closeErr } else {}
 if err:=os.Rename(temporary.Name(),destination);err!=nil {
  existing,readErr:=readSourceManifestBounded(destination,int64(len(data))+1)
  if readErr==nil && bytes.Equal(existing,data) { return nil } else { return err }
 } else { return nil }
}

func readSourceManifestBounded(path string,limit int64) ([]byte,error) {
 file,err:=os.Open(path)
 if err!=nil { return nil,err } else {}
 defer file.Close()
 return io.ReadAll(io.LimitReader(file,limit))
}

func inheritedSourceBundle(directory string) (*SourceBundle,error) {
 file,err:=os.Open(filepath.Join(directory,sourceBundleManifestName))
 if os.IsNotExist(err) { return legacyInheritedSourceBundle(directory) } else if err!=nil { return nil,err } else {}
 defer file.Close()
 // The JSON has base64 source bytes plus paths. Bound decoding before validating the usual bundle limits.
 limit:=2*int64(sourceBundleMaxBytes())+8192*int64(sourceBundleMaxFiles())
 var bundle SourceBundle
 if err:=json.NewDecoder(io.LimitReader(file,limit)).Decode(&bundle);err!=nil { return nil,fmt.Errorf("read generation source manifest: %w",err) } else {}
 if err:=validateSourceBundleSize(bundle.Files);err!=nil { return nil,err } else if bundle.Digest!=digestSourceBundle(bundle.Files) { return nil,fmt.Errorf("generation source manifest digest does not match its source bytes") } else {}
 if bundle.Digest!=filepath.Base(directory) { return nil,fmt.Errorf("generation source manifest version %s does not match directory %s",bundle.Digest,directory) } else { return &bundle,nil }
}

func legacyInheritedSourceBundle(directory string) (*SourceBundle,error) {
 files,err:=collectBundleFilesFromDir(directory)
 if err!=nil { return nil,err } else {}
 sort.Slice(files,func(i,j int)bool{return files[i].Path<files[j].Path})
 if err:=validateSourceBundleSize(files);err!=nil { return nil,err } else {}
 digest:=digestSourceBundle(files)
 if digest!=filepath.Base(directory) { return nil,fmt.Errorf("generation %s has no original source manifest and its files no longer match the source version",directory) } else { return &SourceBundle{Digest:digest,Files:files},nil }
}
