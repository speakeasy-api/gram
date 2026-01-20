package cmd

import "github.com/speakeasy-api/gram/cli/internal/app"

// WhoamiOptions configures the Whoami operation
type WhoamiOptions = app.WhoamiOptions

// WhoamiResult contains the authenticated profile information
type WhoamiResult = app.WhoamiResult

// Whoami returns the current authenticated profile information
var Whoami = app.DoWhoami
