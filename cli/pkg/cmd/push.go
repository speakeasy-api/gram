package cmd

import "github.com/speakeasy-api/gram/cli/internal/app"

// PushOptions configures the Push operation
type PushOptions = app.PushOptions

// PushResult contains the deployment result
type PushResult = app.PushResult

// Push deploys assets to Gram
var Push = app.DoPush
