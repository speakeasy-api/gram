package remotesessionmetrics

// StoredToken names which stored upstream token could not be decrypted.
type StoredToken string

const (
	// StoredTokenAccess is the stored access token.
	StoredTokenAccess StoredToken = "access"

	// StoredTokenRefresh is the stored refresh token.
	StoredTokenRefresh StoredToken = "refresh"
)

// StoredTokenOutcome says whether a refresh replaced an unreadable stored token.
type StoredTokenOutcome string

const (
	// StoredTokenHealed is an unreadable token a refresh replaced, unseen by the person.
	StoredTokenHealed StoredTokenOutcome = "healed"

	// StoredTokenNeedsSignIn is an unreadable token nothing could replace, so the person signs in again.
	StoredTokenNeedsSignIn StoredTokenOutcome = "needs_sign_in"

	// StoredTokenAccessStillLive is an unreadable refresh token met while the access token still serves calls.
	StoredTokenAccessStillLive StoredTokenOutcome = "access_still_live"
)
