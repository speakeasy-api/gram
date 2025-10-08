package bootstrap

import (
	"fmt"
	"path/filepath"
)

func IsReservedPath(workdir, path string) (bool, error) {
	if !filepath.IsAbs(workdir) {
		return false, fmt.Errorf("work dir path is not absolute")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return false, fmt.Errorf("get absolute pah: %w", err)
	}

	rel, err := filepath.Rel(workdir, abs)
	if err != nil || len(rel) > 0 && rel[0] == '.' {
		return true, nil
	}

	noExt := abs[:len(abs)-len(filepath.Ext(abs))]

	switch noExt {
	case filepath.Join(workdir, "gram-start"):
		return true, nil
	case filepath.Join(workdir, "gram_start"):
		return true, nil
	default:
		return false, nil
	}
}
