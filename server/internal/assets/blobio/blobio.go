package blobio

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type Reader interface {
	Read(context.Context, *url.URL) (io.ReadCloser, error)
}

func ReadAllString(ctx context.Context, store Reader, rawURL string, limit int64) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("empty asset URL")
	}
	if store == nil {
		return "", fmt.Errorf("asset storage unavailable")
	}
	assetURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse asset URL: %w", err)
	}
	reader, err := store.Read(ctx, assetURL)
	if err != nil {
		return "", fmt.Errorf("open asset: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return reader.Close() })

	var source io.Reader = reader
	if limit > 0 {
		source = io.LimitReader(reader, limit)
	}
	data, err := io.ReadAll(source)
	if err != nil {
		return "", fmt.Errorf("read asset: %w", err)
	}
	return string(data), nil
}
