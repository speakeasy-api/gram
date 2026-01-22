package cmd

import "github.com/speakeasy-api/gram/cli/internal/app"

type StageFunctionOptions = app.StageFunctionOptions
type StageOpenAPIOptions = app.StageOpenAPIOptions

var StageFunction = app.DoStageFunction
var StageOpenAPI = app.DoStageOpenAPI
