// Package docs embeds the sqlclint rule catalog so the binary can explain any
// diagnostic it emits without needing the repository on disk.
package docs

import "embed"

// Rules holds one markdown document per rule, named "rules/<rule-id>.md". The
// catalog package is responsible for parsing and validating them.
//
//go:embed rules/*.md
var Rules embed.FS
