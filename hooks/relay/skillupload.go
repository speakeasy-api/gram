package relay

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxSkillUploadAPIKeyBytes = 256

// skillUploadTask identifies content the detached worker must reopen. Only the
// credential is private; content never crosses the process boundary.
type skillUploadTask struct {
	ServerURL  string
	Project    string
	APIKey     string
	RawSHA256  string
	SourcePath string
	SourceRoot string
}

var newSkillUploadCommand = func(task skillUploadTask) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable: %w", err)
	}
	cmd := exec.Command(exe, "upload-skill",
		"--server-url="+task.ServerURL,
		"--project="+task.Project,
		"--raw-sha256="+task.RawSHA256,
		"--source-path="+task.SourcePath,
		"--source-root="+task.SourceRoot,
	)
	cmd.Env = envWithoutCredential(os.Environ(), task.APIKey)
	return cmd, nil
}

// startSkillUploadProcess starts a detached child without copying content. The
// child reopens and verifies the manifest, leaving only process creation on the
// gating path. agenthooks.Main calls os.Exit as soon as the handler returns, so
// cmd.Start cannot safely move to a goroutine. The bounded credential is
// prefilled into an anonymous pipe so it remains available after this process
// exits without appearing in argv, the environment, or a file.
var startSkillUploadProcess = func(task skillUploadTask) error {
	cmd, err := newSkillUploadCommand(task)
	if err != nil {
		return err
	}
	credential, err := prefilledSkillUploadCredential(task.APIKey)
	if err != nil {
		return err
	}
	cmd.Stdin = credential
	cmd.SysProcAttr = drainSysProcAttr()
	if err := cmd.Start(); err != nil {
		_ = credential.Close()
		return fmt.Errorf("start skill upload: %w", err)
	}
	closeErr := credential.Close()
	releaseErr := cmd.Process.Release()
	if closeErr != nil {
		return fmt.Errorf("close skill upload credential pipe: %w", closeErr)
	}
	if releaseErr != nil {
		return fmt.Errorf("release skill upload process: %w", releaseErr)
	}
	return nil
}

// startSkillContentUpload hands an upload to a detached process only when the
// final accepted ingest response requests the exact captured content.
func startSkillContentUpload(c creds, res ingestResult, skill *resolvedSkill) error {
	if !res.accepted() || res.skillCapture == nil || !res.skillCapture.contentRequired || skill == nil ||
		!skill.captureReady || res.skillCapture.rawSHA256 != skill.rawSHA256 {
		return nil
	}
	task := skillUploadTask{
		ServerURL:  c.ServerURL,
		Project:    c.Project,
		APIKey:     c.APIKey,
		RawSHA256:  skill.rawSHA256,
		SourcePath: skill.sourcePath,
		SourceRoot: skill.root,
	}
	if !validSkillUploadTask(task) {
		return fmt.Errorf("invalid skill upload task")
	}
	return startSkillUploadProcess(task)
}

// RunSkillUpload reads one bounded credential, reopens one manifest from a
// trusted parent-supplied root, and uploads it only if its full raw hash still
// matches the accepted activation.
func RunSkillUpload(ctx context.Context, args []string, credential io.Reader) int {
	fs := flag.NewFlagSet("upload-skill", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	task := skillUploadTask{}
	fs.StringVar(&task.ServerURL, "server-url", "", "")
	fs.StringVar(&task.Project, "project", "", "")
	fs.StringVar(&task.RawSHA256, "raw-sha256", "", "")
	fs.StringVar(&task.SourcePath, "source-path", "", "")
	fs.StringVar(&task.SourceRoot, "source-root", "", "")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return 1
	}
	key, ok := readSkillUploadCredential(credential)
	if !ok {
		return 1
	}
	task.APIKey = key
	if !validSkillUploadTask(task) {
		return 1
	}
	skill := captureResolvedSkill(&resolvedSkill{}, skillLocation{path: task.SourcePath, level: "", root: task.SourceRoot})
	if !skill.captureReady || skill.rawSHA256 != task.RawSHA256 {
		return 1
	}
	c := creds{ServerURL: task.ServerURL, APIKey: task.APIKey, Project: task.Project, Email: "", Org: "", Source: credEnv}
	if err := newClient(task.ServerURL).uploadSkillContent(ctx, c, task.RawSHA256, skill.content); err != nil {
		return 1
	}
	return 0
}

func validSkillUploadTask(task skillUploadTask) bool {
	if task.APIKey == "" || len(task.APIKey) > maxSkillUploadAPIKeyBytes || strings.ContainsAny(task.APIKey+task.Project, "\r\n") ||
		len(task.RawSHA256) != 64 || !filepath.IsAbs(task.SourcePath) || !filepath.IsAbs(task.SourceRoot) {
		return false
	}
	for _, arg := range []string{task.ServerURL, task.Project, task.RawSHA256, task.SourcePath, task.SourceRoot} {
		if strings.Contains(arg, task.APIKey) {
			return false
		}
	}
	if !validRawSHA256(task.RawSHA256) {
		return false
	}
	u, err := url.Parse(task.ServerURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || insecureServerURL(task.ServerURL) {
		return false
	}
	return true
}

func prefilledSkillUploadCredential(key string) (*os.File, error) {
	if key == "" || len(key) > maxSkillUploadAPIKeyBytes || strings.ContainsAny(key, "\r\n") {
		return nil, fmt.Errorf("invalid skill upload credential")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("open skill upload credential pipe: %w", err)
	}
	if _, err := io.WriteString(writer, key); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return nil, fmt.Errorf("prefill skill upload credential pipe: %w", err)
	}
	if err := writer.Close(); err != nil {
		_ = reader.Close()
		return nil, fmt.Errorf("close skill upload credential writer: %w", err)
	}
	return reader, nil
}

func readSkillUploadCredential(input io.Reader) (string, bool) {
	if input == nil {
		return "", false
	}
	key, err := io.ReadAll(io.LimitReader(input, maxSkillUploadAPIKeyBytes+1))
	if err != nil || len(key) == 0 || len(key) > maxSkillUploadAPIKeyBytes || strings.ContainsAny(string(key), "\r\n") {
		return "", false
	}
	return string(key), true
}

func envWithoutCredential(env []string, credential string) []string {
	result := make([]string, 0, len(env))
	for _, value := range env {
		if credential == "" || !strings.Contains(value, credential) {
			result = append(result, value)
		}
	}
	return result
}
