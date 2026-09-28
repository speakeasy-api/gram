package mcp

import (
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// methodVersions bounds protocol availability independently of which surface
// implements a method. Until is exclusive; an empty bound is open-ended.
type methodVersions struct {
	// Since is the first revision defining the method.
	Since string

	// Until is the first revision removing the method, or empty.
	Until string
}

// methodAvailability is fail-closed: adding a dispatch case also requires an
// explicit protocol policy. Surface-specific switches still own implementation.
var methodAvailability = map[string]methodVersions{
	"initialize":                {Since: mcpversions.Version20241105, Until: mcpversions.Version20260728},
	"notifications/initialized": {Since: mcpversions.Version20241105, Until: mcpversions.Version20260728},
	"ping":                      {Since: mcpversions.Version20241105, Until: mcpversions.Version20260728},
	"server/discover":           {Since: mcpversions.Version20260728, Until: ""},
	"notifications/cancelled":   {Since: mcpversions.Version20241105, Until: ""},
	"tools/list":                {Since: mcpversions.Version20241105, Until: ""},
	"tools/call":                {Since: mcpversions.Version20241105, Until: ""},
	"prompts/list":              {Since: mcpversions.Version20241105, Until: ""},
	"prompts/get":               {Since: mcpversions.Version20241105, Until: ""},
	"resources/list":            {Since: mcpversions.Version20241105, Until: ""},
	"resources/templates/list":  {Since: mcpversions.Version20241105, Until: ""},
	"resources/read":            {Since: mcpversions.Version20241105, Until: ""},
}

// initializeNegotiable reports whether an initialize request's proposed
// params.protocolVersion is eligible for negotiation. A declaration in either
// the header or `_meta` rules that out only when it names a recognized
// revision without initialize (2026-07-28 or later); an unrecognized value on
// initialize comes from a nonconforming client, and the body still wins.
func initializeNegotiable(req *rawRequest, resolution mcpversions.Resolution) bool {
	if req.Method != "initialize" {
		return false
	}
	for _, declared := range []string{resolution.Declared, mcprequests.DeclaredProtocolVersion("", req.Params)} {
		if mcpversions.AtLeast(declared, mcpversions.Version20260728) {
			return false
		}
	}
	return true
}

// methodAvailable reports whether the request's method is defined by the
// revision governing it. A negotiable initialize is governed by the revision
// it will negotiate, so it is available only while supported still holds a
// revision that defines initialize.
func methodAvailable(req *rawRequest, resolution mcpversions.Resolution, supported []string) bool {
	revision := resolution.InEffect
	if req.Method == "initialize" {
		if !initializeNegotiable(req, resolution) {
			return false
		}
		negotiated, ok := mcpversions.Negotiate("", supported)
		if !ok {
			return false
		}
		revision = negotiated
	}
	policy, ok := methodAvailability[req.Method]
	return ok && mcpversions.AtLeast(revision, policy.Since) &&
		(policy.Until == "" || !mcpversions.AtLeast(revision, policy.Until))
}

// unavailableMethod never answers a JSON-RPC notification, including an
// unknown method sent without an ID.
func unavailableMethod(req *rawRequest) error {
	if !req.ID.IsSet() {
		return nil
	}
	return oops.E(oops.CodeNotImplemented, nil, "%s: %s", req.Method, oops.MCPCodeMethodNotFound.Message())
}
