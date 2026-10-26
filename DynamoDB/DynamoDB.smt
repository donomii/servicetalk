; DynamoDB — wraps aws-sdk-dynamodb (the same crate exa's services
; use).  Credentials come from the standard AWS chain: env vars
; (AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_SESSION_TOKEN),
; ~/.aws/credentials, IMDS, etc.  Region defaults to AWS_REGION /
; AWS_DEFAULT_REGION; override with setRegion:.
;
; ServiceTalk surface:
;
;   d := DynamoDB new.
;   d setRegion: 'us-east-1'.                              "optional"
;   d getItem: 'my-table' key: '{"id":{"S":"abc"}}'.       "JSON of the AttributeValue map"
;   d putItem: 'my-table' item: '{"id":{"S":"abc"},"v":{"N":"1"}}'.
;   d deleteItem: 'my-table' key: '{"id":{"S":"abc"}}'.
;   d query: 'my-table' keyConditionExpression: 'id = :v'
;            expressionAttributeValues: '{":v":{"S":"abc"}}'.
;   d scan: 'my-table' limit: 100.
;
; AttributeValue shapes follow the DynamoDB wire format
; (`{"S": "..."}`, `{"N": "..."}`, `{"M": {...}}`, etc.) — same as
; what aws-sdk-dynamodb consumes.  Wrapper does the json-to-
; AttributeValue conversion via serde_dynamo if available; for now
; the caller supplies the wire form.

(foreign-class DynamoDB
  (rmethod initialize       initialize)
  (rmethod setRegion:       set_region)
  (rmethod getItem:key:                                       get_item)
  (rmethod putItem:item:                                      put_item)
  (rmethod deleteItem:key:                                    delete_item)
  (rmethod query:keyConditionExpression:expressionAttributeValues:   query_)
  (rmethod scan:limit:                                        scan_))
