; ClickHouse — talks to a ClickHouse server over its HTTP interface.
; Credentials and target URL are set per-instance; nothing global.
;
; ServiceTalk surface:
;
;   ch := ClickHouse new.
;   ch setUrl: 'http://localhost:8123'.
;   ch setUser: 'default'.
;   ch setPassword: ''.
;   ch query: 'SELECT 1 AS n FORMAT JSONEachRow'.    "→ JSONEachRow string"
;   ch exec:  'INSERT INTO t VALUES (1, 2)'.          "→ '' on success"
;   ch queryWithBody: 'INSERT INTO t FORMAT JSONEachRow' body: '{"a":1}'.
;
; query / exec return the raw HTTP body on success.  On HTTP error the
; result is `{"error":"<status>: <body>"}`.  Use FORMAT JSONEachRow
; (or any other FORMAT) inside the SQL string for structured output;
; this wrapper doesn't try to second-guess the row shape.

(foreign-class ClickHouse
  (rmethod initialize       initialize)
  (rmethod setUrl:          set_url)
  (rmethod setUser:         set_user)
  (rmethod setPassword:     set_password)
  (rmethod setDatabase:     set_database)
  (rmethod query:           query)
  (rmethod exec:            exec_)
  (rmethod queryWithBody:body:  query_with_body))
