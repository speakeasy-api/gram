package gram

var (
	GitSHA                    = "dev"
	AssistantRuntimeImageHash = "dev"
)

func shortGitSHA() string {
	if len(GitSHA) >= 8 {
		return GitSHA[:8]
	}
	return GitSHA
}
