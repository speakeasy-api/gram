// Package secretmask holds the placeholders that management reads return in
// place of stored secret values, so every surface masks secrets the same way.
package secretmask

const (
	// Value stands in for a stored secret that has a value. It is fixed, so a
	// read reveals nothing about the secret, not even its length. The
	// dashboard matches this exact string to recognize a saved secret header.
	Value = "***"

	// Empty stands in for a stored secret whose value is empty. Clients
	// should rely on an explicit has-value flag where the API provides one.
	Empty = "<EMPTY>"
)
