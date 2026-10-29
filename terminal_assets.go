package main

import (
 "embed"
 "net/http"
)

//go:embed web/terminal
var terminalAssets embed.FS

func serveTerminalAsset(w http.ResponseWriter,r *http.Request) {
 http.FileServer(http.FS(terminalAssets)).ServeHTTP(w,r)
}
