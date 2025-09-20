package constants

import "regexp"

const (
	ToolTypeHTTP = "http"
)

const (
	SlugPattern = `^[a-z0-9]+(?:[a-z0-9_-]*[a-z0-9])?$`
	SlugMessage = "must be lowercase, alphanumeric and can contain dashes (-) and underscores (_)"
)

var SlugPatternRE = regexp.MustCompile(SlugPattern)
