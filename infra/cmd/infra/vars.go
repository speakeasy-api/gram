package infra

import (
	_ "embed"
)

// Generate this file with `mise run gen:proto`
//
//go:embed descriptors.pb
var descriptors []byte

var (
	GitSHA = "dev"
)

func shortGitSHA() string {
	if len(GitSHA) >= 8 {
		return GitSHA[:8]
	}
	return GitSHA
}
