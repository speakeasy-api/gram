package telemetry

import (
	"maps"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// AttributeRecorder is a utility to set attributes in a map.
type AttributeRecorder map[attr.Key]any

func (h AttributeRecorder) RecordHTTPMethod(method string) {
	h[attr.HTTPRequestMethodKey] = method
}

func (h AttributeRecorder) RecordHTTPServerURL(url string) {
	h[attr.URLFullKey] = url
}

func (h AttributeRecorder) RecordHTTPRoute(route string) {
	h[attr.HTTPRouteKey] = route
}

func (h AttributeRecorder) RecordHTTPStatusCode(code int) {
	h[attr.HTTPResponseStatusCodeKey] = int64(code)
}

func (h AttributeRecorder) RecordHTTPUserAgent(agent string) {
	h[attr.HTTPRequestHeaderUserAgentKey] = agent
}

func (h AttributeRecorder) RecordHTTPDuration(duration float64) {
	h[attr.HTTPServerRequestDurationKey] = duration
}

func (h AttributeRecorder) RecordHTTPRequestHeaders(headers map[string]string, isSensitive bool) {
	if len(headers) == 0 {
		return
	}

	// try to fetch the existing headers - if they dont exist or are nil, create
	// a map
	hMap, ok := h[attr.HTTPRequestHeadersKey].(map[string]string)
	if !ok {
		hMap = make(map[string]string, len(headers))
	}

	for header, v := range headers {
		if isSensitive {
			v = redactToken(v)
		}
		hMap[header] = v
	}

	h[attr.HTTPRequestHeadersKey] = hMap
}

func (h AttributeRecorder) RecordHTTPResponseHeaders(headers map[string]string) {
	if len(headers) == 0 {
		return
	}

	// try to fetch the existing headers - if they dont exist or are nil, create
	// a map
	hMap, ok := h[attr.HTTPResponseHeadersKey].(map[string]string)
	if !ok {
		hMap = make(map[string]string, len(headers))
	}

	maps.Copy(hMap, headers)

	h[attr.HTTPResponseHeadersKey] = hMap
}

func (h AttributeRecorder) RecordHTTPRequestBody(body int64) {
	h[attr.HTTPRequestBodyKey] = body
}

func (h AttributeRecorder) RecordHTTPResponseBody(body int64) {
	h[attr.HTTPResponseBodyKey] = body
}

func (h AttributeRecorder) RecordLogMessageBody(msg string) {
	h[attr.LogBodyKey] = msg
}
