; Main.smt — entry point for the example servicetalk shell.
;
; Spawns one Shell and exposes it over HTTP so the user can drive it
; from a real terminal.  Two endpoints:
;
;   POST /sh
;     body: a single command line, content-type doesn't matter
;     →     plain-text output of `Shell run: line`
;
;   POST /jobs
;     →     same as `Shell run: 'jobs'` — handy for monitoring
;
; A line-oriented terminal wrapper (`prompt.sh` next to this file)
; reads stdin and POSTs each line, giving you a real `> ` prompt.
;
; Try it:
;   ./smt coordinator --port 8000 --main Main \
;       --path examples/shell &
;   ./examples/shell/prompt.sh
;
; Or one-shot via curl:
;   curl -sS -X POST http://localhost:8080/sh --data-binary 'ls /'
;   curl -sS -X POST http://localhost:8080/sh --data-binary 'run sleep 5'
;   curl -sS -X POST http://localhost:8080/sh --data-binary 'jobs'

(class Main
  (vars sh server resp body req out path)

  (method initialize ()
    (set sh (new Shell))
    (set server (new HttpServer))
    (send server listenOn:handler: 8080 (yourself))
    (print "shell listening on :8080  — POST a line to /sh"))

  (method handle: (req)
    (set path (send req at: "path"))
    (if (send path = "/sh")   (return (handleSh: req)))
    (if (send path = "/jobs") (return (textResp: (send sh run: "jobs"))))
    (notFound))

  (method handleSh: (req)
    (set body (send req at: "body"))
    (set out (send sh run: body))
    (textResp: out))

  (method textResp: (s)
    (set resp (new Dictionary))
    (send resp at:put: "status" 200)
    (send resp at:put: "body" s)
    (send resp at:put: "contentType" "text/plain; charset=utf-8")
    resp)

  (method notFound ()
    (set resp (new Dictionary))
    (send resp at:put: "status" 404)
    (send resp at:put: "body" "not found\n")
    (send resp at:put: "contentType" "text/plain; charset=utf-8")
    resp)

  (method doesNotUnderstand: (msg)
    (print "Main: I don't understand" msg)
    nil))
