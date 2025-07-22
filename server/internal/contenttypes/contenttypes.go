package contenttypes

import "regexp"

var (
	jsonRE = regexp.MustCompile(`(?i)\bjson\b`)
	yamlRE = regexp.MustCompile(`(?i)\byaml\b`)
)

func IsJSON(contentType string) bool {
	return jsonRE.MatchString(contentType)
}

func IsYAML(contentType string) bool {
	return yamlRE.MatchString(contentType)
}
