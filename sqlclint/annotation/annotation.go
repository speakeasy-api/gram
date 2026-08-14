// Package annotation parses the "sqlclint:ignore" comments that exempt a query
// from the tenancy rule.
package annotation

import (
	"strings"
)

// Prefix introduces an exemption annotation in a query's comment block.
const Prefix = "sqlclint:ignore"

// Annotation is a parsed exemption.
type Annotation struct {
	// Category is the exemption id named by the annotation. It is validated
	// against the rule catalog by the caller, not here, so a typo is reported as
	// an unknown category rather than silently dropped.
	Category string

	// Reason is the free text after the "--" separator, explaining why this
	// category is true of this query.
	Reason string

	// Line is the 1-indexed line of the annotation within the file.
	Line int
}

// Find returns the exemption annotation in a query's comment block, if any.
//
// comments are the comment lines with their leading "--" already stripped, and
// firstLine is the file line number of comments[0]. The expected form is:
//
//	sqlclint:ignore <category> -- <reason>
//
// A reason may wrap onto the following comment lines; they are joined with
// single spaces. Only the first annotation is returned: a query claiming two
// different exemptions is contradicting itself, and the second is ignored rather
// than silently merged.
func Find(comments []string, firstLine int) (Annotation, bool) {
	for i, line := range comments {
		if !strings.HasPrefix(line, Prefix) {
			continue
		}

		var rest strings.Builder
		rest.WriteString(strings.TrimSpace(strings.TrimPrefix(line, Prefix)))

		// Continuation lines carry the rest of the reason, but must not swallow a
		// following annotation.
		for _, cont := range comments[i+1:] {
			if strings.HasPrefix(cont, Prefix) {
				break
			}
			rest.WriteString(" " + cont)
		}

		category, reason, _ := strings.Cut(rest.String(), "--")

		return Annotation{
			Category: strings.TrimSpace(category),
			Reason:   strings.Join(strings.Fields(reason), " "),
			Line:     firstLine + i,
		}, true
	}

	return Annotation{}, false
}
