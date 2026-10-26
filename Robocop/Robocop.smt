; Robocop — a foreign (Rust-backed) wrapper around the actual robocop
; crate at /Users/jer/exagit/monorepo-robotchecker/rust/services/robocop.
; The wrapper links the real crate; no logic is re-implemented here.
;
; ServiceTalk surface:
;
;   r := Robocop new.                                "uses /tmp/robocop-cache for the on-disk robots cache"
;   r setCacheDir: '/var/cache/robocop'.             "optional — must be called BEFORE the first check"
;   r checkUrls: #('https://example.com/foo' 'https://example.com/bar').
;     → JSON string of { allowed_urls, disallowed_urls, error, crawl_delay_secs }
;   r checkUrls: urls userAgent: 'ExaBot'.
;     → same shape, with the supplied User-Agent
;
; The wrapper drives `Server::new(ServerHandler::new_local(cache_dir))`,
; which is robocop's AWS-free path — uses an on-disk cache + direct
; reqwest fetch.  ServerHandler::new (the AWS-coupled variant) is not
; exposed; if production needs DynamoDB it should be a separate class.

(foreign-class Robocop
  (rmethod initialize         initialize)
  (rmethod setCacheDir:       set_cache_dir)
  (rmethod checkUrls:         check_urls)
  (rmethod checkUrls:userAgent:  check_urls_user_agent))
