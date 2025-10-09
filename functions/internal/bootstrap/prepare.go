package bootstrap

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/speakeasy-api/gram/functions/internal/javascript"
	"github.com/speakeasy-api/gram/functions/internal/o11y"
	"github.com/speakeasy-api/gram/functions/internal/python"
)

func ResolveProgram(language string, workDir string) (string, string, error) {
	entryPath, err := entrypointFilename(workDir, language)
	if err != nil {
		return "", "", fmt.Errorf("get entrypoint filename: %w", err)
	}

	switch language {
	case "javascript", "typescript":
		return "node", entryPath, nil
	case "python":
		return "python", entryPath, nil
	default:
		return "", "", fmt.Errorf("unsupported language: %s", language)
	}
}

func InitializeMachine(ctx context.Context, logger *slog.Logger, language string, codePath string, workDir string) (command string, program string, err error) {
	if !filepath.IsAbs(workDir) {
		return "", "", fmt.Errorf("work dir path is not absolute")
	}

	if err := unzipCode(ctx, logger, codePath, workDir); err != nil {
		return "", "", fmt.Errorf("unzip code: %w", err)
	}

	command, program, err = prepareProgram(workDir, language)
	if err != nil {
		return "", "", fmt.Errorf("prepare program: %w", err)
	}

	// #nosec G302 -- workDir is a directory and needs to be executable to enter it.
	if err := os.Chmod(workDir, 0555); err != nil {
		return "", "", fmt.Errorf("chmod work dir: %w", err)
	}

	return command, program, nil
}

func unzipCode(ctx context.Context, logger *slog.Logger, zipPath string, dest string) error {
	zipFile, err := zip.OpenReader(zipPath)
	if err != nil {
		if zipFile != nil {
			_ = zipFile.Close()
		}
		return fmt.Errorf("%s: open zip file: %w", zipPath, err)
	}
	defer o11y.LogDefer(ctx, logger, func() error { return zipFile.Close() })

	for _, file := range zipFile.File {
		if err := handleZipFile(ctx, logger, file, dest); err != nil {
			return err
		}
	}

	return nil
}

func handleZipFile(ctx context.Context, logger *slog.Logger, file *zip.File, dest string) error {
	path := filepath.Clean(filepath.Join(dest, filepath.Clean(file.Name)))
	reserved, err := IsReservedPath(dest, path)
	if err != nil {
		return fmt.Errorf("check reserved path: %w", err)
	}
	if reserved {
		return fmt.Errorf("%s: reserved path", file.Name)
	}

	if file.FileInfo().IsDir() {
		if err := os.MkdirAll(path, 0555); err != nil {
			return fmt.Errorf("%s: create directory: %w", path, err)
		}
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0555); err != nil {
		return fmt.Errorf("%s: failed to create directory: %w", dir, err)
	}

	fileReader, err := file.Open()
	if err != nil {
		return fmt.Errorf("%s: open file in zip: %w", file.Name, err)
	}
	defer o11y.LogDefer(ctx, logger, func() error { return fileReader.Close() })

	targetFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0444)
	if err != nil {
		return fmt.Errorf("%s: create target file: %w", path, err)
	}
	defer o11y.LogDefer(ctx, logger, func() error { return targetFile.Close() })

	// Limit extraction to 10MiB per file to prevent decompression bombs
	const maxFileSize = 10 * 1024 * 1024
	written, err := io.Copy(targetFile, io.LimitReader(fileReader, maxFileSize))
	if err != nil {
		return fmt.Errorf("%s: extract file: %w", file.Name, err)
	}

	if written < 0 || file.UncompressedSize64 > uint64(written) {
		return fmt.Errorf("%s: file too large (>%d bytes, wrote %d bytes)", file.Name, maxFileSize, written)
	}

	return nil
}

func entrypointFilename(workDir string, language string) (string, error) {
	switch language {
	case "javascript", "typescript":
		return filepath.Join(workDir, "gram-start.js"), nil
	case "python":
		return filepath.Join(workDir, "gram_start.py"), nil
	default:
		return "", fmt.Errorf("unsupported language: %s", language)
	}
}

func prepareProgram(workDir string, language string) (string, string, error) {
	entryPath, err := entrypointFilename(workDir, language)
	if err != nil {
		return "", "", fmt.Errorf("get entrypoint filename: %w", err)
	}

	switch language {
	case "javascript", "typescript":
		if err := os.WriteFile(entryPath, javascript.Entrypoint, 0444); err != nil {
			return "", "", fmt.Errorf("write %s entrypoint (%s): %w", language, entryPath, err)
		}

		functionsPath := filepath.Join(workDir, "functions.js")
		stat, err := os.Stat(functionsPath)
		switch {
		case errors.Is(err, os.ErrNotExist), err == nil && stat.Size() == 0:
			if err := os.WriteFile(functionsPath, javascript.DefaultFunctions, 0444); err != nil {
				return "", "", fmt.Errorf("write %s default functions (%s): %w", language, functionsPath, err)
			}
		case err != nil:
			return "", "", fmt.Errorf("stat %s: %w", functionsPath, err)
		}

		return "node", entryPath, nil
	case "python":
		if err := os.WriteFile(entryPath, python.Entrypoint, 0444); err != nil {
			return "", "", fmt.Errorf("write %s entrypoint (%s): %w", language, entryPath, err)
		}

		functionsPath := filepath.Join(workDir, "functions.py")
		stat, err := os.Stat(functionsPath)
		switch {
		case errors.Is(err, os.ErrNotExist) || (err == nil && stat.Size() == 0):
			if err := os.WriteFile(functionsPath, python.DefaultFunctions, 0444); err != nil {
				return "", "", fmt.Errorf("write %s default functions (%s): %w", language, functionsPath, err)
			}
		case err != nil:
			return "", "", fmt.Errorf("stat %s: %w", functionsPath, err)
		}

		return "python", entryPath, nil
	default:
		return "", "", fmt.Errorf("unsupported language: %s", language)
	}
}
