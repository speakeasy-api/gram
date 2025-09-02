package about

// See main.go for why this file exists.

var (
	openapiDoc []byte
	GitSHA     string = "dev"
)

func SetOpenAPIDoc(doc []byte) {
	openapiDoc = doc
}

func SetGitSHA(sha string) {
	GitSHA = sha
}
