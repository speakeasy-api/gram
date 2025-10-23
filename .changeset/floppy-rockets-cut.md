---
"cli": patch
---

Fixed an issue where Go's http.Client used by CLI was stripping the
`Content-Length` header. This happens when Go cannot determine the content
length from a given `io.Reader`. It will prefer to drop any custom
`Content-Length` header in favor of using chunked transfer encoding. However
this won't work when hitting Gram's assets API which expects an explicit
`Content-Length` header to be on the request.
