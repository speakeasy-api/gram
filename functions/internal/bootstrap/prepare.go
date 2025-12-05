package bootstrap

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hashicorp/go-cleanhttp"
	funcclient "github.com/speakeasy-api/gram/server/gen/functions"

	"github.com/speakeasy-api/gram/functions/internal/auth"
	"github.com/speakeasy-api/gram/functions/internal/javascript"
	"github.com/speakeasy-api/gram/functions/internal/o11y"
	"github.com/speakeasy-api/gram/functions/internal/python"
)

func ResolveProgram(language string, workDir string) (string, []string, error) {
	entryPath, err := entrypointFilename(workDir, language)
	if err != nil {
		return "", nil, fmt.Errorf("get entrypoint filename: %w", err)
	}

	switch language {
	case "javascript", "typescript":
		return "node", []string{"--experimental-strip-types", entryPath}, nil
	case "python":
		return "python", []string{entryPath}, nil
	default:
		return "", nil, fmt.Errorf("unsupported language: %s", language)
	}
}

type InitializeMachineConfig struct {
	Ident        auth.RunnerIdentity
	ServerClient *funcclient.Client
	Language     string
	CodePath     string
	WorkDir      string
}

func InitializeMachine(ctx context.Context, logger *slog.Logger, config InitializeMachineConfig) (command string, args []string, err error) {
	if !filepath.IsAbs(config.WorkDir) {
		return "", nil, fmt.Errorf("work dir path is not absolute")
	}

	codePath, err := resolveLazyFile(ctx, logger, config.Ident, config.ServerClient, config.CodePath)
	if err != nil {
		return "", nil, fmt.Errorf("resolve lazy file: %w", err)
	}

	if err := unzipCode(ctx, logger, codePath, config.WorkDir); err != nil {
		return "", nil, fmt.Errorf("unzip code: %w", err)
	}

	command, args, err = prepareProgram(config.WorkDir, config.Language)
	if err != nil {
		return "", nil, fmt.Errorf("prepare program: %w", err)
	}

	// #nosec G302 -- workDir is a directory and needs to be executable to enter it.
	if err := os.Chmod(config.WorkDir, 0555); err != nil {
		return "", nil, fmt.Errorf("chmod work dir: %w", err)
	}

	return command, args, nil
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

func prepareProgram(workDir string, language string) (string, []string, error) {
	entryPath, err := entrypointFilename(workDir, language)
	if err != nil {
		return "", nil, fmt.Errorf("get entrypoint filename: %w", err)
	}

	switch language {
	case "javascript", "typescript":
		if err := os.WriteFile(entryPath, javascript.Entrypoint, 0444); err != nil {
			return "", nil, fmt.Errorf("write %s entrypoint (%s): %w", language, entryPath, err)
		}

		functionsPath := filepath.Join(workDir, "functions.js")
		stat, err := os.Stat(functionsPath)
		switch {
		case errors.Is(err, os.ErrNotExist), err == nil && stat.Size() == 0:
			if err := os.WriteFile(functionsPath, javascript.DefaultFunctions, 0444); err != nil {
				return "", nil, fmt.Errorf("write %s default functions (%s): %w", language, functionsPath, err)
			}
		case err != nil:
			return "", nil, fmt.Errorf("stat %s: %w", functionsPath, err)
		}

		return "node", []string{"--experimental-strip-types", entryPath}, nil
	case "python":
		if err := os.WriteFile(entryPath, python.Entrypoint, 0444); err != nil {
			return "", nil, fmt.Errorf("write %s entrypoint (%s): %w", language, entryPath, err)
		}

		functionsPath := filepath.Join(workDir, "functions.py")
		stat, err := os.Stat(functionsPath)
		switch {
		case errors.Is(err, os.ErrNotExist) || (err == nil && stat.Size() == 0):
			if err := os.WriteFile(functionsPath, python.DefaultFunctions, 0444); err != nil {
				return "", nil, fmt.Errorf("write %s default functions (%s): %w", language, functionsPath, err)
			}
		case err != nil:
			return "", nil, fmt.Errorf("stat %s: %w", functionsPath, err)
		}

		return "python", []string{entryPath}, nil
	default:
		return "", nil, fmt.Errorf("unsupported language: %s", language)
	}
}

func resolveLazyFile(ctx context.Context, logger *slog.Logger, ident auth.RunnerIdentity, serverClient *funcclient.Client, filename string) (string, error) {
	var rootCause error
	stat, err := os.Stat(filename)
	switch {
	case errors.Is(err, os.ErrNotExist):
		rootCause = err
		// fall through to check for .lazy file
	case err != nil:
		return "", fmt.Errorf("stat %s: %w", filename, err)
	default:
		if stat.IsDir() {
			return "", fmt.Errorf("path is a directory: %s", filename)
		}
		return filename, nil
	}

	lazy := filename + ".lazy"
	assetID, err := os.ReadFile(lazy)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if rootCause != nil {
			return "", fmt.Errorf("file does not exist: %s: %w", filename, rootCause)
		}
		return "", fmt.Errorf("file does not exist: %s: %w", lazy, rootCause)
	case err != nil:
		return "", fmt.Errorf("read %s: %w", lazy, err)
	}

	if len(assetID) == 0 {
		return "", fmt.Errorf("read asset id %s: empty file", lazy)
	}

	token, err := auth.NewServerJWT(ident, jwt.MapClaims{})
	if err != nil {
		return "", fmt.Errorf("create server jwt: %w", err)
	}

	pres, err := serverClient.GetSignedAssetURL(ctx, &funcclient.GetSignedAssetURLPayload{
		FunctionToken: &token,
		AssetID:       string(assetID),
	})
	if err != nil {
		return "", fmt.Errorf("get signed asset url %s: %w", assetID, err)
	}

	res, err := cleanhttp.DefaultClient().Get(pres.URL)
	if err != nil {
		return "", fmt.Errorf("download asset %s: %w", assetID, err)
	}
	defer o11y.LogDefer(ctx, logger, func() error {
		if err := res.Body.Close(); err != nil {
			return fmt.Errorf("close asset %s response body: %w", assetID, err)
		}
		return nil
	})

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected response code %s: %s", assetID, res.Status)
	}

	outFile, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0444)
	if err != nil {
		return "", fmt.Errorf("create target file %s: %w", filename, err)
	}
	defer o11y.LogDefer(ctx, logger, func() error {
		if err := outFile.Close(); err != nil {
			return fmt.Errorf("close %s: %w", filename, err)
		}
		return nil
	})

	_, err = io.Copy(outFile, res.Body)
	if err != nil {
		return "", fmt.Errorf("write asset to file %s: %w", filename, err)
	}

	return filename, nil
}
