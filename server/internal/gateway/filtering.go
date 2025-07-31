package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"slices"

	"github.com/itchyny/gojq"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contenttypes"
)

type responseFilteringResult struct {
	resp        io.Reader
	statusCode  int
	contentType string
}

func handleResponseFiltering(ctx context.Context, logger *slog.Logger, tool *HTTPTool, responseFilterRequest *ResponseFilterRequest, resp *http.Response) *responseFilteringResult {
	if tool.ResponseFilter == nil || responseFilterRequest == nil {
		return nil
	}

	ctx, filterSpan := trace.SpanFromContext(ctx).TracerProvider().Tracer("github.com/speakeasy-api/gram/server/internal/gateway").Start(ctx, "filterResponse")
	defer filterSpan.End()

	contentType := resp.Header.Get("Content-Type")
	statusCode := resp.StatusCode

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		logger.ErrorContext(ctx, "failed to parse content type", attr.SlogError(err), attr.SlogHTTPResponseHeaderContentType(contentType))
		return nil
	}

	if !slices.Contains(tool.ResponseFilter.ContentTypes, mediaType) || !slices.Contains(tool.ResponseFilter.StatusCodes, fmt.Sprintf("%d", statusCode)) {
		return nil
	}

	query, err := gojq.Parse(responseFilterRequest.Filter)
	if err != nil {
		logger.ErrorContext(ctx, "failed to parse response filter", attr.SlogError(err), attr.SlogFilterExpression(responseFilterRequest.Filter))
		return nil
	}

	buf := bytes.NewBuffer(nil)

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		filterSpan.SetStatus(codes.Error, err.Error())
		logger.ErrorContext(ctx, "failed to read response body", attr.SlogError(err))
		return &responseFilteringResult{
			resp:        buf,
			statusCode:  http.StatusInternalServerError,
			contentType: "application/octet-stream",
		}
	}

	var respData any
	if err := yaml.Unmarshal(data, &respData); err != nil {
		filterSpan.SetStatus(codes.Error, err.Error())
		logger.ErrorContext(ctx, "failed to unmarshal response body", attr.SlogError(err))
		return &responseFilteringResult{
			resp:        buf,
			statusCode:  http.StatusInternalServerError,
			contentType: "application/octet-stream",
		}
	}

	var results []any

	iter := query.RunWithContext(ctx, respData)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			var haltErr *gojq.HaltError
			if errors.As(err, &haltErr) && haltErr.Value() == nil {
				break
			}
			filterSpan.SetStatus(codes.Error, err.Error())
			logger.ErrorContext(ctx, "failed to run response filter", attr.SlogError(err), attr.SlogFilterExpression(responseFilterRequest.Filter))

			// Return error response when filter doesn't match response structure
			errorResponse := map[string]string{
				"error": fmt.Sprintf("Response filter failed to match response structure: %s", err.Error()),
			}
			if encodeErr := json.NewEncoder(buf).Encode(errorResponse); encodeErr != nil {
				logger.ErrorContext(ctx, "failed to encode filter error response", attr.SlogError(encodeErr))
			}
			return &responseFilteringResult{
				resp:        buf,
				statusCode:  http.StatusBadRequest,
				contentType: "application/json",
			}
		}
		results = append(results, v)
	}

	if contenttypes.IsJSON(mediaType) {
		if err := json.NewEncoder(buf).Encode(results); err != nil {
			filterSpan.SetStatus(codes.Error, err.Error())
			logger.ErrorContext(ctx, "failed to encode response filter results", attr.SlogError(err))
		}
	} else if contenttypes.IsYAML(mediaType) {
		if err := yaml.NewEncoder(buf).Encode(results); err != nil {
			filterSpan.SetStatus(codes.Error, err.Error())
			logger.ErrorContext(ctx, "failed to encode response filter results", attr.SlogError(err))
		}
	}

	return &responseFilteringResult{
		resp:        buf,
		statusCode:  statusCode,
		contentType: contentType,
	}
}
