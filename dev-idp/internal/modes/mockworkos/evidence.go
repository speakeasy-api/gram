package mockworkos

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

// Sign-in evidence knobs, so each Speakeasy sign-in tier is testable locally:
//
//	DEVIDP_IDP_HOSTED_DOMAIN=<domain>    Google/Microsoft managed domain (default: the user's email domain; "none" = personal account)
//	DEVIDP_IDP_TENANT_ID=<id>            Microsoft tenant (default: a fixed work tenant; "consumer" = personal account)
const (
	devTenantID      = "00000000-0000-4000-8000-0000000000aa"
	consumerTenantID = "9188040d-6c67-4c5b-b112-36a304b66dad"
	googleTokenStem  = "mock_google_"
)

func hostedDomain(email string) string {
	switch v := os.Getenv("DEVIDP_IDP_HOSTED_DOMAIN"); v {
	case "none":
		return ""
	case "":
		if at := strings.LastIndexByte(email, '@'); at >= 0 {
			return strings.ToLower(email[at+1:])
		}
		return ""
	default:
		return strings.ToLower(v)
	}
}

func tenantID() string {
	switch v := os.Getenv("DEVIDP_IDP_TENANT_ID"); v {
	case "":
		return devTenantID
	case "consumer":
		return consumerTenantID
	default:
		return v
	}
}

// oauthTokens mirrors the provider tokens WorkOS returns for social sign-ins.
func oauthTokens(method, email string) map[string]any {
	var token string
	switch method {
	case "GoogleOAuth":
		token = googleTokenStem + base64.RawURLEncoding.EncodeToString([]byte(hostedDomain(email)))
	case "MicrosoftOAuth":
		local := email
		if at := strings.LastIndexByte(email, '@'); at >= 0 {
			local = email[:at]
		}
		upn := local + "@" + hostedDomain(email)
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
		payload, _ := json.Marshal(map[string]string{"tid": tenantID(), "upn": upn})
		token = header + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
	default:
		return nil
	}
	return map[string]any{
		"access_token":  token,
		"refresh_token": randomToken(),
		"scopes":        []string{"openid", "email", "profile"},
		"expires_at":    time.Now().Add(time.Hour).Unix(),
	}
}

// handleGoogleUserInfo mocks Google's OpenID Connect userinfo endpoint for the
// tokens oauthTokens hands out.
func (h *Handler) handleGoogleUserInfo(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "+googleTokenStem)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token"})
		return
	}
	hd, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token"})
		return
	}
	info := map[string]any{"email_verified": true}
	if len(hd) > 0 {
		info["hd"] = string(hd)
	}
	writeJSON(w, http.StatusOK, info)
}
