package idjag

import "time"

const (
	// Type is the protected JWT header type for an ID-JAG.
	Type = "oauth-id-jag+jwt"

	// MaxLifetime bounds how far an ID-JAG expiration can be in the future.
	MaxLifetime = 10 * time.Minute

	maxBytes = 32 * 1024
)
