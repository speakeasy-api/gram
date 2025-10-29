---
"function-runners": patch
---

Fixed an issue where certain allowed headers in Gram Functions response were interfering with the Trailers that were being set containing resource usage metrics. By removing the Content-Length header from the response, we ensure that the Trailers can be set and read correctly by the client. Previously, setting both headers would prevent response body bytes from being sent back to the server.
