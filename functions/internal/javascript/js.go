package javascript

import (
	_ "embed"
)

//go:embed gram-start.mjs
var Entrypoint []byte
