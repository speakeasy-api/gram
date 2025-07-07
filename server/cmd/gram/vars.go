package gram

var (
	GitSHA = "dev"
)

func shortGitSHA() string {
	if len(GitSHA) >= 8 {
		return GitSHA[:8]
	}
	return GitSHA
}
