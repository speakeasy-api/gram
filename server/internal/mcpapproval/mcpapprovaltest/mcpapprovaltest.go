// Package mcpapprovaltest wires the MCP approval service for tests in other
// packages without reaching real registries, MCP servers, or Temporal.
package mcpapprovaltest

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/advisories"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/authority"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/capability"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/catalog"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/domainmeta"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/evidence"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/exposure"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/packagemeta"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repometa"
)

// NewAssembler returns the real evidence assembler over traffic. Its registry,
// advisory, probe, and catalog sources answer as if they know nothing about
// the target.
func NewAssembler(traffic exposure.Reader) *evidence.Assembler {
	return evidence.NewAssembler(
		packagemeta.NewClient(notFoundRegistry{}),
		repometa.NewClient(notFoundRegistry{}),
		advisories.NewClient(emptyAdvisoryDB{}),
		domainmeta.NewClient(notFoundRegistry{}),
		traffic,
		QuietProbes{},
		QuietProbes{},
		QuietProbes{},
	)
}

// StartResearch implements mcpapproval.ResearchStarter without a Temporal
// worker.
func StartResearch(context.Context, mcpapproval.ResearchRun) error {
	return nil
}

// QuietProbes stands in for the remote probes and catalog: discovery finds
// nothing, tools/list returns no declarations, and no catalog entry matches.
type QuietProbes struct{}

func (QuietProbes) DiscoverAuthority(context.Context, string) (*authority.Declaration, error) {
	return nil, nil
}

func (QuietProbes) ListToolDeclarations(context.Context, string) ([]capability.Declaration, error) {
	return nil, nil
}

func (QuietProbes) Lookup(context.Context, uuid.UUID, string, bool) (*catalog.Match, error) {
	return nil, nil
}

type notFoundRegistry struct{}

func (notFoundRegistry) Do(request *http.Request) (*http.Response, error) {
	return &http.Response{
		Status:           http.StatusText(http.StatusNotFound),
		StatusCode:       http.StatusNotFound,
		Body:             io.NopCloser(strings.NewReader(`{"error":"Not found"}`)),
		Header:           http.Header{},
		Request:          request,
		Proto:            "HTTP/1.1",
		ProtoMajor:       1,
		ProtoMinor:       1,
		ContentLength:    -1,
		TransferEncoding: nil,
		Close:            false,
		Uncompressed:     false,
		Trailer:          nil,
		TLS:              nil,
	}, nil
}

// emptyAdvisoryDB answers every advisory query with an empty document, OSV's
// shape for a package it has nothing on.
type emptyAdvisoryDB struct{}

func (emptyAdvisoryDB) Do(request *http.Request) (*http.Response, error) {
	return &http.Response{
		Status:           http.StatusText(http.StatusOK),
		StatusCode:       http.StatusOK,
		Body:             io.NopCloser(strings.NewReader(`{}`)),
		Header:           http.Header{},
		Request:          request,
		Proto:            "HTTP/1.1",
		ProtoMajor:       1,
		ProtoMinor:       1,
		ContentLength:    -1,
		TransferEncoding: nil,
		Close:            false,
		Uncompressed:     false,
		Trailer:          nil,
		TLS:              nil,
	}, nil
}
