package gcpkms

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// publishLocalKeyFile writes doc to path with mode 0600, but only if path does
// not exist yet. The atomic link makes concurrent server and worker startups
// converge on one key: the loser gets an error wrapping fs.ErrExist and must
// read the winner's file instead of replacing a key another process loaded.
func publishLocalKeyFile(path string, doc []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create persistent local key directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".local-kms-*.tmp")
	if err != nil {
		return fmt.Errorf("create persistent local key temporary file: %w", err)
	}
	tmpName := tmp.Name()
	removeTemp := func() error {
		return errors.Join(tmp.Close(), os.Remove(tmpName))
	}
	if err := tmp.Chmod(0o600); err != nil {
		return errors.Join(fmt.Errorf("secure persistent local key temporary file: %w", err), removeTemp())
	}
	if _, err := tmp.Write(doc); err != nil {
		return errors.Join(fmt.Errorf("write persistent local key temporary file: %w", err), removeTemp())
	}
	if err := tmp.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync persistent local key temporary file: %w", err), removeTemp())
	}
	if err := tmp.Close(); err != nil {
		return errors.Join(fmt.Errorf("close persistent local key temporary file: %w", err), os.Remove(tmpName))
	}

	if err := os.Link(tmpName, path); err != nil {
		return errors.Join(fmt.Errorf("publish persistent local key: %w", err), os.Remove(tmpName))
	}
	// The key is published, so a leftover temporary name is only a second link
	// to it and must not fail startup.
	_ = os.Remove(tmpName)

	// Sync the directory so a crash cannot lose the new link and leave the next
	// start to generate a different key.
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("sync persistent local key directory: %w", err)
	}

	return nil
}

func syncDir(dir string) error {
	handle, err := os.Open(dir) //nolint:gosec // dir is the application-selected local-development key directory.
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	if err := handle.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync: %w", err), handle.Close())
	}
	if err := handle.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	return nil
}

// readLocalKeyFile reads a persistent local key, refusing anything but a
// regular file only its owner can access.
func readLocalKeyFile(path string) ([]byte, error) {
	file, err := os.Open(path) //nolint:gosec // path is an application-selected local-development config file, not request input.
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("inspect: %w", err), file.Close())
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.Join(errors.New("must be a regular file accessible only to its owner"), file.Close())
	}
	doc, readErr := io.ReadAll(file)
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	return doc, nil
}
