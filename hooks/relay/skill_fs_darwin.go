//go:build darwin

package relay

import "golang.org/x/sys/unix"

func renameDirNoReplace(oldPath, newPath string) error {
	return unix.RenamexNp(oldPath, newPath, unix.RENAME_EXCL)
}
