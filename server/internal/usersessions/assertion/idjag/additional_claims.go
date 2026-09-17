package idjag

type additionalClaims struct {
	Resource string `json:"resource"`
	ClientID string `json:"client_id"`
	Email    string `json:"email"`
	Scope    string `json:"scope"`
}
