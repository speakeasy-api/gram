package otel

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/otel/gramotel"
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
	// publish hands the decoded export to gramotel's publishing core, which
	// validates every item before publishing any and settles every publish
	// before returning. A gramotel.ErrInvalid error is the exporter's fault.
	publish func(context.Context, []M) error
}

// ingestOTLPExport owns the transport contract shared by OTLP signals:
// authenticated tenancy, bounded decompression, decoding. Validation,
// publishing and the durability wait belong to gramotel's publishing core,
// so the exporter is acknowledged only once the whole export is on the topic.
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

	if err := spec.publish(ctx, items); err != nil {
		if errors.Is(err, gramotel.ErrInvalid) {
			return oops.E(oops.CodeBadRequest, err, "invalid OTLP %s export", spec.signal).LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "unable to accept OTLP %s export", spec.signal).LogError(ctx, logger)
	}

	return nil
}
