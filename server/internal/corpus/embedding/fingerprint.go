package embedding

// Fingerprint computes a SHA-256 hash of the content, strategy, metadata, and
// manifest fingerprint. Chunks with identical fingerprints can skip re-embedding.
func Fingerprint(content string, strategy string, metadata string, manifestFingerprint string) string {
	panic("not implemented")
}
