package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HttpClient is a built-in class.  Each instance is its own OS process
// (like Array, Dictionary, etc.) and exposes a small surface for fetching
// HTTP resources.  Used by user classes that need to talk to the world
// — robots.txt fetching, S3 / DynamoDB SigV4 calls, gRPC-over-HTTP/2,
// and so on — without having to reach into Go via a foreign-class
// subprocess.
//
// All methods return a Dictionary with the same shape:
//
//   status   number  HTTP status code; 0 on transport / setup error
//   body     string  response body (empty on transport / setup error)
//   error    string  short error message; nil on success
//
// Returning a Dictionary instead of bare values keeps the caller from
// having to remember separate `lastStatus` / `lastError` instance vars
// and lets a single client be shared safely across concurrent fetches.

const (
	httpClientDefaultTimeoutMs    = 30_000
	httpClientMaxResponseBodyByte = 16 * 1024 * 1024 // 16 MiB cap; robocop only needs a few KB
)

// callHttpClientMethod handles every send to an HttpClient instance.
// The method names mirror Smalltalk-y selectors so user code feels at
// home with the rest of the runtime.
func (obj *ObjectServer) callHttpClientMethod(name string, args []Value) Value {
	switch name {
	case "initialize":
		return Nil // built-in: nothing to set up
	case "class":
		return StrVal(UserClassHttpClient)
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
		case "get:", "getWithHeaders:headers:", "getWithTimeout:timeoutMs:",
			"post:body:", "post:body:contentType:",
			"put:body:", "put:body:contentType:",
			"patch:body:contentType:", "delete:":
			return True
		}
		return False
	case "inspect":
		obj.transcriptf("HttpClient")
		return Nil
	case "free":
		obj.deregister()
		return Nil

	case "get:":
		if len(args) < 1 {
			return obj.httpClientErrorDict("get: requires a URL string")
		}
		return obj.httpClientDo("GET", args[0].String(), nil, "", httpClientDefaultTimeoutMs, nil)

	case "getWithTimeout:timeoutMs:", "get:timeoutMs:": // "get:timeoutMs:" kept for compat
		if len(args) < 2 {
			return obj.httpClientErrorDict("getWithTimeout:timeoutMs: requires URL and timeout")
		}
		timeout := httpClientDefaultTimeoutMs
		if args[1].Kind == "number" {
			timeout = int(args[1].Num)
		}
		return obj.httpClientDo("GET", args[0].String(), nil, "", timeout, nil)

	case "getWithHeaders:headers:", "get:headers:": // "get:headers:" kept for compat
		if len(args) < 2 {
			return obj.httpClientErrorDict("getWithHeaders:headers: requires URL and headers Dictionary")
		}
		hdrs := obj.dictionaryToHeaders(args[1])
		return obj.httpClientDo("GET", args[0].String(), nil, "", httpClientDefaultTimeoutMs, hdrs)

	case "post:body:":
		if len(args) < 2 {
			return obj.httpClientErrorDict("post:body: requires URL and body string")
		}
		body := []byte(args[1].String())
		return obj.httpClientDo("POST", args[0].String(), body, "application/octet-stream", httpClientDefaultTimeoutMs, nil)

	case "post:body:contentType:":
		if len(args) < 3 {
			return obj.httpClientErrorDict("post:body:contentType: requires URL, body, content-type")
		}
		body := []byte(args[1].String())
		ct := args[2].String()
		return obj.httpClientDo("POST", args[0].String(), body, ct, httpClientDefaultTimeoutMs, nil)

	case "put:body:":
		if len(args) < 2 {
			return obj.httpClientErrorDict("put:body: requires URL and body string")
		}
		body := []byte(args[1].String())
		return obj.httpClientDo("PUT", args[0].String(), body, "application/octet-stream", httpClientDefaultTimeoutMs, nil)

	case "put:body:contentType:":
		if len(args) < 3 {
			return obj.httpClientErrorDict("put:body:contentType: requires URL, body, content-type")
		}
		body := []byte(args[1].String())
		ct := args[2].String()
		return obj.httpClientDo("PUT", args[0].String(), body, ct, httpClientDefaultTimeoutMs, nil)

	case "patch:body:contentType:":
		if len(args) < 3 {
			return obj.httpClientErrorDict("patch:body:contentType: requires URL, body, content-type")
		}
		body := []byte(args[1].String())
		ct := args[2].String()
		return obj.httpClientDo("PATCH", args[0].String(), body, ct, httpClientDefaultTimeoutMs, nil)

	case "delete:":
		if len(args) < 1 {
			return obj.httpClientErrorDict("delete: requires a URL string")
		}
		return obj.httpClientDo("DELETE", args[0].String(), nil, "", httpClientDefaultTimeoutMs, nil)
	}

	obj.transcriptf("HttpClient doesNotUnderstand: %s", name)
	return Nil
}

// httpClientDo runs the actual HTTP roundtrip and returns a Dictionary
// with status / body / error fields.  Body is truncated at
// httpClientMaxResponseBodyByte to keep a runaway server from blowing
// the heap; the truncation is reflected in the error field.
func (obj *ObjectServer) httpClientDo(method, url string, body []byte, contentType string, timeoutMs int, extraHeaders map[string]string) Value {
	if url == "" {
		return obj.httpClientErrorDict("empty URL")
	}
	if timeoutMs <= 0 {
		timeoutMs = httpClientDefaultTimeoutMs
	}

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return obj.httpClientErrorDict(fmt.Sprintf("bad request: %v", err))
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return obj.httpClientResultDict(0, "", err.Error(), nil)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, httpClientMaxResponseBodyByte+1)
	respBody, err := io.ReadAll(limited)
	if err != nil {
		return obj.httpClientResultDict(resp.StatusCode, "", fmt.Sprintf("read body: %v", err), nil)
	}
	truncated := false
	if len(respBody) > httpClientMaxResponseBodyByte {
		respBody = respBody[:httpClientMaxResponseBodyByte]
		truncated = true
	}

	errStr := ""
	if truncated {
		errStr = fmt.Sprintf("body truncated at %d bytes", httpClientMaxResponseBodyByte)
	}

	// Capture response headers (first value per canonical key).
	respHeaders := make(map[string]string, len(resp.Header))
	for k, vs := range resp.Header {
		if len(vs) > 0 {
			respHeaders[k] = vs[0]
		}
	}
	return obj.httpClientResultDict(resp.StatusCode, string(respBody), errStr, respHeaders)
}

// httpClientResultDict spawns a Dictionary and fills in status / body /
// error / headers.  The dict is returned as an ObjVal so the caller can
// send it `at:`, `at:put:`, etc.  Pass a nil respHeaders map on error
// paths — the key is still present but set to nil.
func (obj *ObjectServer) httpClientResultDict(status int, body, errStr string, respHeaders map[string]string) Value {
	dictID := obj.spawnBuiltin(BuiltinClassDictionary)
	if dictID == "" {
		obj.transcriptf("HttpClient: failed to spawn result dictionary")
		return Nil
	}
	ref := ObjVal(dictID)
	obj.sendMessage(ref, "at:put:", []Value{StrVal("status"), NumVal(float64(status))})
	obj.sendMessage(ref, "at:put:", []Value{StrVal("body"), StrVal(body)})
	if errStr == "" {
		obj.sendMessage(ref, "at:put:", []Value{StrVal("error"), Nil})
	} else {
		obj.sendMessage(ref, "at:put:", []Value{StrVal("error"), StrVal(errStr)})
	}

	// Populate a nested Dictionary for response headers.
	if len(respHeaders) > 0 {
		hdrsID := obj.spawnBuiltin(BuiltinClassDictionary)
		if hdrsID != "" {
			hdrsRef := ObjVal(hdrsID)
			for k, v := range respHeaders {
				obj.sendMessage(hdrsRef, "at:put:", []Value{StrVal(k), StrVal(v)})
			}
			obj.sendMessage(ref, "at:put:", []Value{StrVal("headers"), hdrsRef})
		}
	} else {
		obj.sendMessage(ref, "at:put:", []Value{StrVal("headers"), Nil})
	}
	return ref
}

func (obj *ObjectServer) httpClientErrorDict(msg string) Value {
	return obj.httpClientResultDict(0, "", msg, nil)
}

// dictionaryToHeaders pulls header pairs out of a Smalltalk Dictionary
// passed in as `args[N]`.  Any non-string value is rendered with
// Value.String() so callers can pass numbers (e.g. Content-Length).
// Returns an empty map for non-objid arguments — keeps callers from
// having to special-case nil.
func (obj *ObjectServer) dictionaryToHeaders(v Value) map[string]string {
	out := map[string]string{}
	if v.Kind != "objid" {
		return out
	}
	keys := obj.sendMessage(v, "keys", nil)
	if keys.Kind != "objid" {
		return out
	}
	size := obj.sendMessage(keys, "size", nil)
	n := int(size.Num)
	for i := 0; i < n; i++ {
		k := obj.sendMessage(keys, "at:", []Value{NumVal(float64(i))})
		val := obj.sendMessage(v, "at:", []Value{k})
		out[strings.TrimSpace(k.String())] = val.String()
	}
	return out
}
