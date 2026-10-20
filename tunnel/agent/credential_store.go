package agent

// credentialStore holds session token files for one agent instance.
type credentialStore interface {
	// createSession makes an empty, private session directory.
	createSession() (credentialDir, error)

	// Close removes the instance's storage and releases its lock.
	Close() error
}

// credentialDir is one session's private directory.
type credentialDir interface {
	// writeToken atomically replaces the session's token file.
	writeToken(token string) error

	// tokenPath is the absolute path of the token file the server reads.
	tokenPath() string

	// homePath is the absolute path of the server's private home directory.
	homePath() string

	// remove deletes the directory and everything in it.
	remove() error
}
