package telemetry

import (
	"maps"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// HTTPLogAttributes is a utility to set attributes in a map.
type HTTPLogAttributes map[attr.Key]any

func (h HTTPLogAttributes) RecordMethod(method string) {
	h[attr.HTTPRequestMethodKey] = method
}

func (h HTTPLogAttributes) RecordServerURL(url string, toolType repo.ToolType) {
	// currently we only want to record this server URL for HTTP tool types
	// Not exposing fly function details unnecessarily
	if toolType == repo.ToolTypeHTTP {
		h[attr.URLFullKey] = url
	}
}

func (h HTTPLogAttributes) RecordRoute(route string) {
	h[attr.HTTPRouteKey] = route
}

func (h HTTPLogAttributes) RecordStatusCode(code int) {
	h[attr.HTTPResponseStatusCodeKey] = int64(code)
}

func (h HTTPLogAttributes) RecordUserAgent(agent string) {
	h[attr.HTTPRequestHeaderUserAgentKey] = agent
}

func (h HTTPLogAttributes) RecordDuration(duration float64) {
	h[attr.HTTPServerRequestDurationKey] = duration
}

func (h HTTPLogAttributes) RecordRequestHeaders(headers map[string]string, isSensitive bool) {
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

func (h HTTPLogAttributes) RecordResponseHeaders(headers map[string]string) {
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

func (h HTTPLogAttributes) RecordRequestBody(body int64) {
	h[attr.HTTPRequestBodyKey] = body
}

func (h HTTPLogAttributes) RecordResponseBody(body int64) {
	h[attr.HTTPResponseBodyKey] = body
}

func (h HTTPLogAttributes) RecordMessageBody(msg string) {
	h[attr.LogBodyKey] = msg
}
