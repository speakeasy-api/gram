package admin

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// UserSearchTerm is a literal search value scoped to any/name/email/org.
type UserSearchTerm struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

// ParseUserSearch parses the bounded, AND-only admin user search grammar.
func ParseUserSearch(query string) ([]UserSearchTerm, error) {
	if len(query) > 2048 {
		return nil, errors.New("Search must be at most 2048 UTF-8 bytes.")
	}
	chars := []rune(query)
	terms := make([]UserSearchTerm, 0)
	for i := 0; i < len(chars); {
		if unicode.IsSpace(chars[i]) {
			i++
			continue
		}
		if len(terms) == 20 {
			return nil, errors.New("Search supports at most 20 terms.")
		}
		field := "any"
		start := i
		for i < len(chars) && !unicode.IsSpace(chars[i]) && chars[i] != '"' && chars[i] != ':' {
			i++
		}
		if i < len(chars) && chars[i] == ':' {
			field = strings.ToLower(string(chars[start:i]))
			if strings.ContainsAny(field, "()") || strings.HasPrefix(field, "-") || strings.HasPrefix(field, "!") {
				return nil, errors.New("Unsupported search syntax; quote it to search for literal text.")
			}
			if field != "name" && field != "email" && field != "org" {
				return nil, errors.New("Unknown search field; use name:, email:, or org:.")
			}
			i++
		} else {
			i = start
		}
		var value strings.Builder
		quoted := i < len(chars) && chars[i] == '"'
		if quoted {
			i++
			for i < len(chars) && chars[i] != '"' {
				if chars[i] == '\\' && i+1 < len(chars) && (chars[i+1] == '\\' || chars[i+1] == '"') {
					i++
				}
				value.WriteRune(chars[i])
				i++
			}
			if i == len(chars) {
				return nil, errors.New("Unclosed quote in search term.")
			}
			i++
			if i < len(chars) && !unicode.IsSpace(chars[i]) {
				return nil, errors.New("Separate search terms with whitespace; quote the whole value.")
			}
		} else {
			for i < len(chars) && !unicode.IsSpace(chars[i]) {
				if chars[i] == '"' {
					return nil, errors.New("Separate search terms with whitespace; quote the whole value.")
				}
				value.WriteRune(chars[i])
				i++
			}
		}
		text := value.String()
		if text == "" {
			return nil, errors.New("Search terms must have a value.")
		}
		if !quoted && (strings.EqualFold(text, "OR") || strings.EqualFold(text, "NOT") || strings.ContainsAny(text, "()") || strings.HasPrefix(text, "-") || strings.HasPrefix(text, "!") || (strings.HasPrefix(text, "/") && strings.HasSuffix(text, "/"))) {
			return nil, errors.New("Unsupported search syntax; quote it to search for literal text.")
		}
		if utf8.RuneCountInString(text) > 256 {
			return nil, errors.New("Search terms must be at most 256 Unicode code points.")
		}
		terms = append(terms, UserSearchTerm{Field: field, Value: text})
	}
	return terms, nil
}
