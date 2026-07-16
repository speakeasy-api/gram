//go:build windows

package relay

import "golang.org/x/sys/windows"

func renameDirNoReplace(oldPath, newPath string) error {
	oldPathPtr, err := windows.UTF16PtrFromString(oldPath)
	if err != nil {
		return err
	}
	newPathPtr, err := windows.UTF16PtrFromString(newPath)
	if err != nil {
		return err
	}
	return windows.MoveFile(oldPathPtr, newPathPtr)
}
