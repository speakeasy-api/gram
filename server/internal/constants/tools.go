package constants

import "regexp"

const (
	ToolTypeHTTP = "http"
)

const (
	SlugPattern = `^[a-z0-9_-]{1,128}$`
	SlugMessage = "must be lowercase, alphanumeric and can contain dashes (-) and underscores (_)"
)

var SlugPatternRE = regexp.MustCompile(SlugPattern)
