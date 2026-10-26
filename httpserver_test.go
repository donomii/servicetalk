package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHttpServerRequestFields(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/check?source=test", strings.NewReader(`{"urls":[]}`))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	fields := httpServerRequestFields(request, []byte(`{"urls":[]}`), 1234567890)

	wants := map[string]Value{
		"method":               StrVal(http.MethodPost),
		"path":                 StrVal("/check"),
		"query":                StrVal("source=test"),
		"body":                 StrVal(`{"urls":[]}`),
		"bodyByteLength":       NumVal(11),
		"contentType":          StrVal("application/json"),
		"receivedAtUnixMillis": NumVal(1234567890),
	}
	for key, want := range wants {
		if got := fields[key]; got != want {
			t.Errorf("request field %q = %#v, want %#v", key, got, want)
		}
	}
}

func TestHttpServerRequestFieldsRejectsInvalidContentType(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.Header.Set("Content-Type", "not a media type;")
	fields := httpServerRequestFields(request, nil, 1)
	if got := fields["contentType"]; got != StrVal("") {
		t.Fatalf("contentType = %#v, want empty string for an invalid media type", got)
	}
}

func TestHttpServerListenAssignsAvailablePort(t *testing.T) {
	server := newTestObject(BuiltinClassHttpServer)
	if got := server.httpServerListen(NumVal(0), ObjVal("handler"), httpServerDefaultHandlerMethod); !got.Bool {
		t.Fatal("listenOn: 0 failed")
	}
	state := server.getHttpServerState()
	state.mu.Lock()
	port := state.port
	state.mu.Unlock()
	server.httpServerStop()
	if port <= 0 || port > 65535 {
		t.Fatalf("assigned port = %d, want 1..65535", port)
	}
}
