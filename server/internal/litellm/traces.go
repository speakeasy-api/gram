package litellm

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/codes"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"goa.design/goa/v3/security"
	"google.golang.org/protobuf/proto"

	gen "github.com/speakeasy-api/gram/server/gen/litellm"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const maxOTLPBodyBytes = 4 * 1024 * 1024

var (
	errOTLPBodyTooLarge = errors.New("LiteLLM OTLP body exceeds 4 MiB")
	errTooManyOTLPSpans = errors.New("OTLP trace export contains too many spans")
)

func (s *Service) traceHTTPHandler() http.Handler {
	return oops.ErrHandle(s.logger, s.serveTracesHTTP)
}

func (s *Service) serveTracesHTTP(w http.ResponseWriter, r *http.Request) (retErr error) {
	ctx, span := s.tracer.Start(r.Context(), "litellm.traces")
	defer func() {
		if retErr != nil {
			span.SetStatus(codes.Error, retErr.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}()

	ctx, err := s.authenticateOTLPRequest(ctx, r.Header)
	if err != nil {
		return err
	}
	version := ""
	defer func() {
		s.health.Record(ctx, healthSignalOTEL, version, retErr)
	}()

	mediaType, body, err := readOTLPRequest(r)
	if err != nil {
		return err
	}

	var request *otlpExportRequest
	switch mediaType {
	case "application/json":
		request, err = decodeOTLPJSON(body)
	default:
		protobufRequest := &collectortracev1.ExportTraceServiceRequest{ResourceSpans: nil}
		if preflightErr := preflightOTLPProtobuf(body); preflightErr != nil {
			err = preflightErr
		} else if unmarshalErr := proto.Unmarshal(body, protobufRequest); unmarshalErr != nil {
			err = fmt.Errorf("decode OTLP protobuf: %w", unmarshalErr)
		} else if structureErr := validateProtoOTLPStructure(protobufRequest); structureErr != nil {
			err = structureErr
		} else {
			request = exportRequestFromProto(protobufRequest)
		}
	}
	if err != nil {
		if errors.Is(err, errTooManyOTLPSpans) {
			return oops.E(oops.CodeRequestTooLarge, err, "OTLP trace export contains too many spans")
		}
		return oops.E(oops.CodeBadRequest, err, "invalid OTLP trace export")
	}
	version = traceReportedVersion(request)
	if err := s.ingestTraceExport(ctx, request); err != nil {
		return err
	}
	w.WriteHeader(http.StatusAccepted)
	return nil
}

func traceReportedVersion(request *otlpExportRequest) string {
	if request == nil {
		return ""
	}
	for _, resourceSpans := range request.ResourceSpans {
		if resourceSpans.Resource != nil {
			name := otlpStringAttribute(resourceSpans.Resource.Attributes, "service.name")
			version := otlpStringAttribute(resourceSpans.Resource.Attributes, "service.version")
			if version != "" && strings.EqualFold(name, "litellm") {
				return version
			}
		}
		for _, scopeSpans := range resourceSpans.ScopeSpans {
			if scopeSpans.Scope != nil && strings.Contains(strings.ToLower(scopeSpans.Scope.Name), "litellm") && strings.TrimSpace(scopeSpans.Scope.Version) != "" {
				return scopeSpans.Scope.Version
			}
		}
	}
	return ""
}

func otlpStringAttribute(values []otlpKeyValue, key string) string {
	for _, value := range values {
		if value.Key == key && value.Value.StringValue != nil {
			return strings.TrimSpace(*value.Value.StringValue)
		}
	}
	return ""
}

func readOTLPRequest(r *http.Request) (string, []byte, error) {
	contentTypes := r.Header.Values("Content-Type")
	if len(contentTypes) > 1 {
		return "", nil, oops.E(oops.CodeBadRequest, nil, "Content-Type must be provided exactly once")
	}
	contentType := ""
	if len(contentTypes) == 1 {
		contentType = contentTypes[0]
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", nil, oops.E(oops.CodeUnsupportedMedia, err, "unsupported OTLP content type")
	}
	if mediaType != "application/json" && mediaType != "application/x-protobuf" && mediaType != "application/protobuf" {
		return "", nil, oops.E(oops.CodeUnsupportedMedia, nil, "unsupported OTLP content type")
	}
	contentEncodings := r.Header.Values("Content-Encoding")
	if len(contentEncodings) > 1 {
		return "", nil, oops.E(oops.CodeBadRequest, nil, "Content-Encoding must not be repeated")
	}
	contentEncoding := ""
	if len(contentEncodings) == 1 {
		contentEncoding = contentEncodings[0]
	}
	contentEncoding, err = validateOTLPContentEncoding(contentEncoding)
	if err != nil {
		return "", nil, oops.E(oops.CodeUnsupportedMedia, err, "unsupported OTLP content encoding")
	}

	body, err := readOTLPBody(r, contentEncoding)
	if err != nil {
		if errors.Is(err, errOTLPBodyTooLarge) {
			return "", nil, oops.E(oops.CodeRequestTooLarge, err, "OTLP request is too large")
		}
		return "", nil, oops.E(oops.CodeBadRequest, err, "invalid OTLP request body")
	}
	return mediaType, body, nil
}

func (s *Service) authenticateOTLPRequest(ctx context.Context, header http.Header) (context.Context, error) {
	// The canonical-route dispatch runs this same authenticator to pick a
	// handler; a context carrying a litellm-named key can only come from that
	// pass, so skip the redundant authorize round trip.
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil && strings.HasPrefix(authCtx.APIKeyName, auth.LiteLLMAPIKeyNamePrefix) {
		return ctx, nil
	}
	keyScheme := &security.APIKeyScheme{
		Name:           constants.KeySecurityScheme,
		Scopes:         []string{"consumer", "producer", "chat", "hooks", "agent", "agent_user"},
		RequiredScopes: []string{"hooks"},
	}
	ctx, err := s.APIKeyAuth(ctx, credentialHeader(header.Get(constants.APIKeyHeader)), keyScheme)
	if err != nil {
		return ctx, err
	}

	projectScheme := &security.APIKeyScheme{
		Name:           constants.ProjectSlugSecuritySchema,
		Scopes:         []string{},
		RequiredScopes: []string{"hooks"},
	}
	return s.APIKeyAuth(ctx, credentialHeader(header.Get(constants.ProjectHeader)), projectScheme)
}

func credentialHeader(value string) string {
	value = strings.TrimSpace(value)
	if _, credential, ok := strings.Cut(value, " "); ok {
		return strings.TrimSpace(credential)
	}
	return value
}

var errUnsupportedOTLPEncoding = errors.New("unsupported LiteLLM OTLP content encoding")

func validateOTLPContentEncoding(value string) (string, error) {
	switch encoding := strings.ToLower(strings.TrimSpace(value)); encoding {
	case "", "gzip":
		return encoding, nil
	default:
		return "", errUnsupportedOTLPEncoding
	}
}

func readOTLPBody(r *http.Request, contentEncoding string) ([]byte, error) {
	compressed, err := readLimitedOTLPBody(r.Body)
	if err != nil {
		return nil, err
	}
	switch contentEncoding {
	case "":
		return compressed, nil
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			return nil, fmt.Errorf("open gzip OTLP body: %w", err)
		}
		decompressed, readErr := readLimitedOTLPBody(reader)
		o11y.NoLogDefer(reader.Close)
		if readErr != nil {
			return nil, readErr
		}
		return decompressed, nil
	default:
		return nil, errUnsupportedOTLPEncoding
	}
}

func readLimitedOTLPBody(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxOTLPBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read OTLP body: %w", err)
	}
	if len(body) > maxOTLPBodyBytes {
		return nil, errOTLPBodyTooLarge
	}
	return body, nil
}

func (s *Service) Traces(ctx context.Context, payload *gen.TracesPayload) error {
	if payload == nil {
		return oops.E(oops.CodeBadRequest, nil, "trace payload is required")
	}
	body, err := json.Marshal(struct {
		ResourceSpans []any `json:"resourceSpans"`
	}{ResourceSpans: payload.ResourceSpans})
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid OTLP trace export")
	}
	request, err := decodeOTLPJSON(body)
	if err != nil {
		if errors.Is(err, errTooManyOTLPSpans) {
			return oops.E(oops.CodeRequestTooLarge, err, "OTLP trace export contains too many spans")
		}
		return oops.E(oops.CodeBadRequest, err, "invalid OTLP trace export")
	}
	return s.ingestTraceExport(ctx, request)
}

func (s *Service) ingestTraceExport(ctx context.Context, request *otlpExportRequest) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "unauthorized")
	}
	params := s.traceLogParams(ctx, request, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())
	if len(params) == 0 {
		return nil
	}
	enrichAcceptedTelemetryAttribution(ctx, s.instances, authCtx, params)
	s.traces.Enqueue(ctx, params)
	return nil
}
