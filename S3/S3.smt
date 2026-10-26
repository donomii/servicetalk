; S3 — wraps aws-sdk-s3.  Credentials/region from the standard AWS
; chain (env vars, ~/.aws/credentials, IMDS); setRegion: overrides.
;
; ServiceTalk surface:
;
;   s := S3 new.
;   s setRegion: 'us-east-1'.                              "optional"
;   s getObject: 'my-bucket' key: 'path/to/file.txt'.      "→ string body, or {error:…}"
;   s putObject: 'my-bucket' key: 'path/foo.txt' body: 'hello'.
;   s deleteObject: 'my-bucket' key: 'path/foo.txt'.
;   s listObjects: 'my-bucket' prefix: 'path/'.            "→ JSON Array of keys"
;
; Bodies are passed in/out as strings (UTF-8 bytes).  For binary
; payloads you can still round-trip them since servicetalk strings
; are byte-shaped, but the encoding is up to you (e.g. base64).

(foreign-class S3
  (rmethod initialize       initialize)
  (rmethod setRegion:       set_region)
  (rmethod getObject:key:                    get_object)
  (rmethod putObject:key:body:               put_object)
  (rmethod deleteObject:key:                 delete_object)
  (rmethod listObjects:prefix:               list_objects))
