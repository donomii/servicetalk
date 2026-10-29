package main

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// HttpServer is a built-in class.  Each instance owns one net/http
// listener and forwards every incoming request to a user-supplied
// handler object via Smalltalk message-passing.  Used by services that
// want to expose themselves outside the cluster — robocop's CheckUrls
// REST endpoint is the worked example.
//
// Surface:
//
//   (set s (new HttpServer))
//   (send s listenOn:handler: 8080 myHandler)   ; non-blocking
//   (send s stop)
//
// The handler receives one method call per request:
//
//   (method handle: (req)
//     ; req is a Dictionary with: method, path, query, body,
//     ; contentType, bodyByteLength, receivedAtUnixMillis
//     (set resp (new Dictionary))
//     (send resp at:put: "status" 200)
//     (send resp at:put: "body" "hello")
//     resp)
//
// Both the request and response are Dictionaries — see the keys
// listed below.  The body field is always a string.

const (
	httpServerDefaultReadTimeout   = 15 * time.Second
	httpServerDefaultWriteTimeout  = 30 * time.Second
	httpServerMaxRequestBodyByte   = 16 * 1024 * 1024 // 16 MiB
	httpServerDefaultHandlerMethod = "handle:"
)

// httpServerState carries the live listener for an HttpServer instance.
// Stored as a pointer field on ObjectServer so the methods below have a
// stable place to put the listener and the user-supplied handler ref.
type httpServerState struct {
	mu            sync.Mutex
	server        *http.Server
	port          int
	handler       Value  // ObjVal of the user's handler object
	handlerMethod string // method name to call on the handler (default "handle:")
	started       bool
}

// callHttpServerMethod handles every send to an HttpServer instance.
func (obj *ObjectServer) callHttpServerMethod(name string, args []Value) Value {
	switch name {
	case "initialize":
		return Nil // built-in: lazy state init in listenOn:
	case "class":
		return StrVal(UserClassHttpServer)
	case "yourself":
		return ObjVal(obj.id)
	case "isNil":
		return False
	case "notNil":
		return True
	case "respondsTo:":
		if len(args) < 1 {
			return False
		}
		switch args[0].String() {
		case "listenOn:handler:", "listenOn:handler:methodName:", "stop", "isRunning", "port", "free":
			return True
		}
		return False
	case "inspect":
		st := obj.getHttpServerState()
		st.mu.Lock()
		obj.transcriptf("HttpServer port=%d running=%v", st.port, st.started)
		st.mu.Unlock()
		return Nil

	case "listenOn:handler:":
		if len(args) < 2 {
			return obj.httpServerSignal("listenOn:handler: requires (port handler)")
		}
		return obj.httpServerListen(args[0], args[1], httpServerDefaultHandlerMethod)

	case "listenOn:handler:methodName:":
		if len(args) < 3 {
			return obj.httpServerSignal("listenOn:handler:methodName: requires (port handler methodName)")
		}
		method := args[2].String()
		if method == "" {
			method = httpServerDefaultHandlerMethod
		}
		return obj.httpServerListen(args[0], args[1], method)

	case "stop":
		return obj.httpServerStop()

	case "isRunning":
		st := obj.getHttpServerState()
		st.mu.Lock()
		defer st.mu.Unlock()
		return BoolVal(st.started)

	case "port":
		st := obj.getHttpServerState()
		st.mu.Lock()
		defer st.mu.Unlock()
		return NumVal(float64(st.port))

	case "free":
		obj.httpServerStop()
		obj.deregister()
		return Nil
	}

	return unsupportedMessage(ObjVal(obj.id), "HttpServer", name, args)
}

// getHttpServerState lazily initialises and returns the listener state.
// We store it as a pointer field so callers can read fields under the
// state's own mutex without the broader ObjectServer mutex.
func (obj *ObjectServer) getHttpServerState() *httpServerState {
	if obj.httpServerState == nil {
		obj.httpServerState = &httpServerState{}
	}
	return obj.httpServerState
}

// httpServerListen starts the listener.  Idempotent against the same
// (port, handler) pair: a second call with the same args is a no-op,
// while a call with different args returns an error string.
func (obj *ObjectServer) httpServerListen(portVal, handlerVal Value, handlerMethod string) Value {
	if portVal.Kind != "number" {
		return obj.httpServerSignal("listenOn:: port must be a number")
	}
	if handlerVal.Kind != "objid" {
		return obj.httpServerSignal("listenOn:: handler must be an object")
	}
	port := int(portVal.Num)
	if port < 0 || port > 65535 {
		return obj.httpServerSignal(fmt.Sprintf("listenOn:: invalid port %d", port))
	}

	st := obj.getHttpServerState()
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.started {
		if (st.port == port || port == 0) && st.handler.ObjID == handlerVal.ObjID && st.handlerMethod == handlerMethod {
			return True // idempotent re-listen with identical config
		}
		return obj.httpServerSignal(fmt.Sprintf("HttpServer already listening on port %d; stop it first", st.port))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		obj.httpServerHandleRequest(w, r)
	})
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return obj.httpServerSignal(fmt.Sprintf("listenOn:: listen on port %d failed: %v", port, err))
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	srv := &http.Server{
		Addr:         listener.Addr().String(),
		Handler:      mux,
		ReadTimeout:  httpServerDefaultReadTimeout,
		WriteTimeout: httpServerDefaultWriteTimeout,
	}
	st.server = srv
	st.port = actualPort
	st.handler = handlerVal
	st.handlerMethod = handlerMethod
	st.started = true

	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			obj.transcriptf("HttpServer listen failed on :%d — %v", actualPort, err)
			st.mu.Lock()
			st.started = false
			st.mu.Unlock()
		}
	}()
	return True
}

// httpServerStop shuts the listener down and clears state so listenOn:
// can be called again.
func (obj *ObjectServer) httpServerStop() Value {
	st := obj.getHttpServerState()
	st.mu.Lock()
	srv := st.server
	wasStarted := st.started
	st.started = false
	st.server = nil
	st.mu.Unlock()
	if !wasStarted || srv == nil {
		return False
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	return True
}

// httpServerHandleRequest is invoked from the listener goroutine on
// every incoming request.  It builds a request Dictionary, calls the
// user's handler synchronously, then writes the response Dictionary
// back to the wire.
func (obj *ObjectServer) httpServerHandleRequest(w http.ResponseWriter, r *http.Request) {
	receivedAtUnixMillis := time.Now().UnixMilli()
	st := obj.getHttpServerState()
	st.mu.Lock()
	handler := st.handler
	handlerMethod := st.handlerMethod
	st.mu.Unlock()

	if handler.Kind != "objid" {
		http.Error(w, "no handler", http.StatusInternalServerError)
		return
	}

	if r.ContentLength > httpServerMaxRequestBodyByte {
		http.Error(w, fmt.Sprintf("request body exceeds %d bytes", httpServerMaxRequestBodyByte), http.StatusRequestEntityTooLarge)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, httpServerMaxRequestBodyByte+1))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(body) > httpServerMaxRequestBodyByte {
		http.Error(w, fmt.Sprintf("request body exceeds %d bytes", httpServerMaxRequestBodyByte), http.StatusRequestEntityTooLarge)
		return
	}

	reqDictID := obj.spawnBuiltin(BuiltinClassDictionary)
	if reqDictID == "" {
		http.Error(w, "spawn req dict failed", http.StatusInternalServerError)
		return
	}
	reqDict := ObjVal(reqDictID)
	for key, value := range httpServerRequestFields(r, body, receivedAtUnixMillis) {
		obj.sendMessage(reqDict, "at:put:", []Value{StrVal(key), value})
	}

	resp := obj.sendMessage(handler, handlerMethod, []Value{reqDict})

	status := http.StatusOK
	respBody := ""
	contentType := "application/json"
	if resp.Kind == "objid" {
		s := obj.sendMessage(resp, "at:", []Value{StrVal("status")})
		if s.Kind == "number" && s.Num > 0 {
			status = int(s.Num)
		}
		b := obj.sendMessage(resp, "at:", []Value{StrVal("body")})
		if b.Kind == "string" {
			respBody = b.Str
		} else if b.Kind != "nil" {
			respBody = b.String()
		}
		ct := obj.sendMessage(resp, "at:", []Value{StrVal("contentType")})
		if ct.Kind == "string" && ct.Str != "" {
			contentType = ct.Str
		}
	} else if resp.Kind == "string" {
		respBody = resp.Str
		contentType = "text/plain; charset=utf-8"
	} else if resp.Kind == "nil" {
		status = http.StatusNoContent
		contentType = ""
	} else {
		respBody = resp.String()
		contentType = "text/plain; charset=utf-8"
	}

	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(respBody)))
	w.WriteHeader(status)
	_, _ = w.Write([]byte(respBody))
}

func httpServerRequestFields(r *http.Request, body []byte, receivedAtUnixMillis int64) map[string]Value {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		contentType = ""
	}
	return map[string]Value{
		"method":               StrVal(r.Method),
		"path":                 StrVal(r.URL.Path),
		"query":                StrVal(r.URL.RawQuery),
		"body":                 StrVal(string(body)),
		"bodyByteLength":       NumVal(float64(len(body))),
		"contentType":          StrVal(contentType),
		"receivedAtUnixMillis": NumVal(float64(receivedAtUnixMillis)),
	}
}

func (obj *ObjectServer) httpServerSignal(msg string) Value {
	obj.transcriptf("HttpServer: %s", msg)
	return False
}
