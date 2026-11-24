package transcriptstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// SealedAssetSuffix is appended to a plaintext object's path to name its
// sealed copy. The copy sits next to the plaintext object, so prefix-based
// retention and bucket policies cover both.
const SealedAssetSuffix = ".sealed"

const (
	// meterAssetCaptureDuration is the time to seal and write one sealed
	// asset object.
	meterAssetCaptureDuration = "chat.transcript_shadow.asset_capture.duration"
	// meterAssetCaptureObjects counts asset objects submitted for sealing, by
	// kind and outcome.
	meterAssetCaptureObjects = "chat.transcript_shadow.asset_capture.objects"
	// meterAssetReadDuration is the time to look up, read and open one sealed
	// asset object.
	meterAssetReadDuration = "chat.transcript_shadow.asset_read.duration"
	// meterAssetReadObjects counts asset reads by kind and outcome. Objects
	// with no sealed copy count as plaintext_fallback.
	meterAssetReadObjects = "chat.transcript_shadow.asset_read.objects"
)

// AssetBlobs is the slice of the asset blob store the sealed asset store uses.
// It matches assets.BlobStore without importing it.
type AssetBlobs interface {
	Exists(ctx context.Context, objectURL *url.URL) (bool, error)
	Read(ctx context.Context, objectURL *url.URL) (io.ReadCloser, error)
	Write(ctx context.Context, objectPath string, contentType string, contentLength int64) (io.WriteCloser, *url.URL, error)
}

// AssetStore keeps encrypted copies of transcript asset objects (large message
// bodies, content parts and chat attachments) next to their plaintext objects.
// Like message shadows, sealed copies never replace or remove plaintext.
type AssetStore struct {
	blobs   AssetBlobs
	service *orgencryption.Service
	tracer  trace.Tracer
	metrics *assetMetrics
}

func NewAssetStore(blobs AssetBlobs, service *orgencryption.Service, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider) (*AssetStore, error) {
	if blobs == nil || service == nil || tracerProvider == nil || meterProvider == nil {
		return nil, ErrInvalid
	}
	metrics, err := newAssetMetrics(meterProvider)
	if err != nil {
		return nil, err
	}
	return &AssetStore{
		blobs:   blobs,
		service: service,
		tracer:  tracerProvider.Tracer(instrumentationName),
		metrics: metrics,
	}, nil
}

// SealedAssetPath names the sealed copy of the plaintext object at objectPath.
func SealedAssetPath(objectPath string) string { return objectPath + SealedAssetSuffix }

// SealedAssetURL names the sealed copy of the plaintext object at objectURL.
// Every blob backend returns a URL whose path ends with the written object
// path, so this matches SealedAssetPath.
func SealedAssetURL(objectURL *url.URL) *url.URL {
	sealed := *objectURL
	if sealed.Opaque != "" {
		// A relative object path parses back as an opaque URL, such as
		// file:name.txt.
		sealed.Opaque += SealedAssetSuffix
		return &sealed
	}
	sealed.Path += SealedAssetSuffix
	sealed.RawPath = ""
	return &sealed
}

// AssetContentHash returns the content hash a content-addressed object name
// carries, such as "<sha256>.json" or "<sha256>.txt".
func AssetContentHash(objectURL *url.URL) (string, bool) {
	if objectURL == nil {
		return "", false
	}
	name := path.Base(objectURL.Path)
	hash, _, _ := strings.Cut(name, ".")
	if !transcriptcrypto.IsContentHash(hash) {
		return "", false
	}
	return hash, true
}

// Seal encrypts body for identity and writes it next to the plaintext object
// at objectPath. Writes are create-only-if-absent where the backend supports
// it, and any valid sealed copy of the same bytes and identity is equivalent,
// so concurrent writers of the same content-addressed object agree.
func (s *AssetStore) Seal(ctx context.Context, organizationID string, identity transcriptcrypto.AssetIdentity, objectPath string, body []byte) error {
	if s == nil {
		return ErrInvalid
	}
	if organizationID == "" || objectPath == "" {
		return ErrInvalid
	}

	ctx, span := s.tracer.Start(ctx, "transcriptstore.SealAsset", trace.WithAttributes(
		attr.ProjectID(identity.ProjectID.String()),
		attr.OrganizationID(organizationID),
		attr.TranscriptShadowAssetKind(string(identity.Kind)),
	))
	defer span.End()

	start := time.Now()
	err := s.seal(ctx, organizationID, identity, objectPath, body)
	result := classify(err)
	s.metrics.capture.record(ctx, identity, time.Since(start), result)
	span.SetAttributes(attr.Outcome(result))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, string(result))
	}
	return err
}

func (s *AssetStore) seal(ctx context.Context, organizationID string, identity transcriptcrypto.AssetIdentity, objectPath string, body []byte) error {
	codec, err := transcriptcrypto.New(s.service, organizationID)
	if err != nil {
		return safe(err)
	}
	sealed, err := codec.EncryptAsset(ctx, identity, body)
	if err != nil {
		return safe(err)
	}
	w, _, err := s.blobs.Write(ctx, SealedAssetPath(objectPath), "application/octet-stream", int64(len(sealed)))
	if err != nil {
		return safe(err)
	}
	if _, err := io.Copy(w, bytes.NewReader(sealed)); err != nil {
		_ = w.Close()
		return safe(err)
	}
	if err := w.Close(); err != nil {
		return safe(err)
	}
	return nil
}

// Open reads the sealed copy of the plaintext object at objectURL. found is
// false when no sealed copy exists, and the caller serves plaintext. A sealed
// copy that exists but fails authentication, belongs to another identity or
// holds other bytes than its content hash fails the read: it is never skipped
// in favour of plaintext. maxBody bounds the opened body.
func (s *AssetStore) Open(ctx context.Context, organizationID string, identity transcriptcrypto.AssetIdentity, objectURL *url.URL, maxBody int64) (body []byte, found bool, err error) {
	if s == nil {
		return nil, false, ErrInvalid
	}
	if organizationID == "" || objectURL == nil || maxBody < 1 {
		return nil, false, ErrInvalid
	}

	ctx, span := s.tracer.Start(ctx, "transcriptstore.OpenAsset", trace.WithAttributes(
		attr.ProjectID(identity.ProjectID.String()),
		attr.OrganizationID(organizationID),
		attr.TranscriptShadowAssetKind(string(identity.Kind)),
	))
	defer span.End()

	start := time.Now()
	body, found, err = s.open(ctx, organizationID, identity, objectURL, maxBody)
	result := classify(err)
	if err == nil && !found {
		result = outcomePlaintextFallback
	}
	s.metrics.read.record(ctx, identity, time.Since(start), result)
	span.SetAttributes(attr.Outcome(result))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, string(result))
		return nil, false, err
	}
	return body, found, nil
}

func (s *AssetStore) open(ctx context.Context, organizationID string, identity transcriptcrypto.AssetIdentity, objectURL *url.URL, maxBody int64) ([]byte, bool, error) {
	sealedURL := SealedAssetURL(objectURL)
	exists, err := s.blobs.Exists(ctx, sealedURL)
	if err != nil {
		return nil, false, safe(err)
	}
	if !exists {
		return nil, false, nil
	}
	r, err := s.blobs.Read(ctx, sealedURL)
	if err != nil {
		return nil, false, safe(err)
	}
	defer func() { _ = r.Close() }()

	// The envelope base64-encodes the framed body, so a sealed object is
	// about 4/3 of its body plus a small header and envelope prefix.
	limit := maxBody/3*4 + 8*1024
	sealed, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, false, safe(err)
	}
	if int64(len(sealed)) > limit {
		return nil, false, ErrStale
	}
	codec, err := transcriptcrypto.New(s.service, organizationID)
	if err != nil {
		return nil, false, safe(err)
	}
	body, err := codec.DecryptAsset(ctx, identity, sealed)
	if errors.Is(err, transcriptcrypto.ErrAssetMismatch) {
		return nil, false, ErrStale
	}
	if err != nil {
		return nil, false, safe(err)
	}
	// The read limit above allows for envelope overhead, so it does not bound
	// the opened body exactly.
	if int64(len(body)) > maxBody {
		return nil, false, ErrStale
	}
	return body, true, nil
}

// RefuseUnverifiable is for a reader that cannot name the identity of the
// plaintext object at objectURL, for example because its row lacks an
// organization or its object name is not a content hash. Without an encrypted
// copy the reader serves plaintext. With one, it returns ErrStale: a copy that
// exists is never skipped in favour of plaintext.
func (s *AssetStore) RefuseUnverifiable(ctx context.Context, objectURL *url.URL) error {
	exists, err := s.HasSealedCopy(ctx, objectURL)
	if err != nil {
		return err
	}
	if exists {
		return ErrStale
	}
	return nil
}

// HasSealedCopy reports whether the plaintext object at objectURL has an
// encrypted copy, without opening it.
func (s *AssetStore) HasSealedCopy(ctx context.Context, objectURL *url.URL) (bool, error) {
	if s == nil || objectURL == nil {
		return false, ErrInvalid
	}
	exists, err := s.blobs.Exists(ctx, SealedAssetURL(objectURL))
	if err != nil {
		return false, safe(err)
	}
	return exists, nil
}

type assetMetrics struct {
	capture assetOperationMetrics
	read    assetOperationMetrics
}

type assetOperationMetrics struct {
	duration metric.Float64Histogram
	objects  metric.Int64Counter
}

func newAssetMetrics(meterProvider metric.MeterProvider) (*assetMetrics, error) {
	meter := meterProvider.Meter(instrumentationName)
	capture, err := newAssetOperationMetrics(meter, meterAssetCaptureDuration, meterAssetCaptureObjects,
		"Time to seal and write one encrypted transcript asset object",
		"Transcript asset objects submitted for sealing")
	if err != nil {
		return nil, err
	}
	read, err := newAssetOperationMetrics(meter, meterAssetReadDuration, meterAssetReadObjects,
		"Time to look up, read and open one encrypted transcript asset object",
		"Transcript asset objects requested from encrypted copies")
	if err != nil {
		return nil, err
	}
	return &assetMetrics{capture: capture, read: read}, nil
}

func newAssetOperationMetrics(meter metric.Meter, durationName, objectsName, durationDescription, objectsDescription string) (assetOperationMetrics, error) {
	duration, err := meter.Float64Histogram(
		durationName,
		metric.WithDescription(durationDescription),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
	)
	if err != nil {
		return assetOperationMetrics{}, fmt.Errorf("create %s: %w", durationName, err)
	}
	objects, err := meter.Int64Counter(
		objectsName,
		metric.WithDescription(objectsDescription),
		metric.WithUnit("{object}"),
	)
	if err != nil {
		return assetOperationMetrics{}, fmt.Errorf("create %s: %w", objectsName, err)
	}
	return assetOperationMetrics{duration: duration, objects: objects}, nil
}

// record is keyed by project and kind: sealed assets only exist for the same
// short project allowlist as message shadows.
func (m assetOperationMetrics) record(ctx context.Context, identity transcriptcrypto.AssetIdentity, elapsed time.Duration, result outcome) {
	opts := metric.WithAttributes(
		attr.ProjectID(identity.ProjectID.String()),
		attr.TranscriptShadowAssetKind(string(identity.Kind)),
		attr.Outcome(result),
	)
	m.duration.Record(ctx, elapsed.Seconds(), opts)
	m.objects.Add(ctx, 1, opts)
}
