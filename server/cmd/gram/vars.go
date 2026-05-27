package gram

var (
	GitSHA = "dev"
	// AssistantRuntimeImageHash is a content hash of the assistant runtime
	// image sources (agents/), injected via -ldflags at build time. Used as the
	// fly registry image tag so deploys that don't touch runtime sources reuse
	// the existing tag and skip churning machines.
	AssistantRuntimeImageHash = "dev"
)

func shortGitSHA() string {
	if len(GitSHA) >= 8 {
		return GitSHA[:8]
	}
	return GitSHA
}
