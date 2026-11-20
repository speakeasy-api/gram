// Package aivendorstest provides AI client catalog fixtures for tests.
package aivendorstest

import "github.com/speakeasy-api/gram/server/internal/aivendors"

// NewStore returns a store backed by the compiled registry.
func NewStore() *aivendors.Store { return aivendors.NewStore(aivendors.CompiledSnapshot()) }
