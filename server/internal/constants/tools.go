package constants

import "regexp"

const SlugPattern = `^[a-z0-9]+(?:[a-z0-9_-]*[a-z0-9])?$`

var SlugPatternRE = regexp.MustCompile(SlugPattern)
