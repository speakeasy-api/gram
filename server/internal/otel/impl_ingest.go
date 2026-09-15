package otel

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type otlpIngestTenant struct {
	organizationID string
	projectID      string
}

type otlpIngestSpec[M any] struct {
	signal          string
	contentEncoding *string
	body            io.ReadCloser
	decode          func([]byte, otlpIngestTenant) ([]M, error)
	validate        func(M) error
	publisher       gcp.Publisher[M]
}

// ingestOTLPExport owns the transport and durability contract shared by OTLP
// signals. Signal-specific callers provide only tree decoding and item
// validation; this function authenticates tenancy, bounds decompression,
// validates the complete export before publishing any prefix, and settles every
// queued publish before acknowledging the exporter.
func ingestOTLPExport[M any](ctx context.Context, logger *slog.Logger, spec otlpIngestSpec[M]) (err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	defer o11y.NoLogDefer(func() error { return spec.body.Close() })

	// Both the encoded body and, for a compressed one, what it expands to are
	// capped: the size of an export as sent says nothing about how much of it a
	// gzip stream unpacks into.
	reader := io.LimitReader(spec.body, maxOTLPExportBytes+1)
	switch encoding := strings.ToLower(strings.TrimSpace(conv.PtrValOr(spec.contentEncoding, ""))); encoding {
	case "", "identity":
	case "gzip":
		decompressed, err := gzip.NewReader(reader)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "unable to read gzipped OTLP %s export", spec.signal).LogError(ctx, logger)
		}
		defer o11y.NoLogDefer(func() error { return decompressed.Close() })

		reader = io.LimitReader(decompressed, maxOTLPExportBytes+1)
	default:
		return oops.E(oops.CodeUnsupportedMedia, nil, "unsupported OTLP content encoding %q", encoding)
	}

	raw, err := io.ReadAll(reader)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "unable to read OTLP %s export", spec.signal).LogError(ctx, logger)
	}
	if len(raw) > maxOTLPExportBytes {
		return oops.E(oops.CodeRequestTooLarge, nil, "OTLP %s export exceeds %d MiB", spec.signal, maxOTLPExportBytes/constants.MiB)
	}

	// Tenancy comes from the authenticated request, never from producer-controlled
	// resource attributes.
	tenant := otlpIngestTenant{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      authCtx.ProjectID.String(),
	}
	items, err := spec.decode(raw, tenant)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid OTLP %s export", spec.signal).LogError(ctx, logger)
	}

	for _, item := range items {
		if err := spec.validate(item); err != nil {
			return oops.E(oops.CodeBadRequest, err, "invalid OTLP %s export", spec.signal).LogError(ctx, logger)
		}
	}

	// Enqueue the complete export before settling results so the publisher can
	// flush it as one batch. Waiting for every result makes Pub/Sub durability a
	// precondition of acknowledging the OTLP exporter.
	results := make([]gcp.PublishResult, 0, len(items))
	for _, item := range items {
		results = append(results, spec.publisher.Publish(ctx, item))
	}

	var publishErr error
	for _, result := range results {
		if _, err := result.Get(ctx); err != nil {
			publishErr = errors.Join(publishErr, err)
		}
	}
	if publishErr != nil {
		return oops.E(oops.CodeUnexpected, publishErr, "unable to accept OTLP %s export", spec.signal).LogError(ctx, logger)
	}

	return nil
}
