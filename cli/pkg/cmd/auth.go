package cmd

import "github.com/speakeasy-api/gram/cli/internal/app"

// AuthOptions configures the Auth operation
type AuthOptions = app.AuthOptions

// AuthResult contains the authentication result
type AuthResult = app.AuthResult

// Auth runs the browser OAuth flow via Speakeasy OIDC
var Auth = app.DoAuth
