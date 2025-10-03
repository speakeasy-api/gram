package ipc

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
)

func Mkfifo() (string, func() error, error) {
	if runtime.GOOS == "windows" {
		return "", nil, fmt.Errorf("named pipes on Windows are not supported in this implementation")
	}

	suffix, err := alphanum(8)
	if err != nil {
		return "", nil, fmt.Errorf("generate fifo suffix: %w", err)
	}

	tmpDir := os.TempDir()
	path := filepath.Join(tmpDir, fmt.Sprintf("fifo-%s", suffix))

	err = syscall.Mkfifo(path, 0666)
	if err != nil {
		return "", nil, fmt.Errorf("make fifo %s: %w", path, err)
	}

	cleanup := func() error {
		err := os.Remove(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove fifo %s: %w", path, err)
		}
		return nil
	}

	return path, cleanup, nil
}

func alphanum(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	maxnum := big.NewInt(int64(len(charset)))

	result := make([]byte, length)
	for i := range result {
		num, err := rand.Int(rand.Reader, maxnum)
		if err != nil {
			return "", fmt.Errorf("generate random number: %w", err)
		}
		result[i] = charset[num.Int64()]
	}

	return string(result), nil
}
