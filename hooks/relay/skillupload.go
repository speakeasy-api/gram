package relay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	skillUploadTaskVersion  = 1
	maxSkillUploadTaskBytes = 512 << 10
)

var skillUploadPipeTimeout = 2 * time.Second

// skillUploadTask is the complete, private handoff from the latency-sensitive
// hook process to the detached uploader.
type skillUploadTask struct {
	Version   int    `json:"version"`
	ServerURL string `json:"server_url"`
	Project   string `json:"project"`
	APIKey    string `json:"api_key"`
	RawSHA256 string `json:"raw_sha256"`
	Content   string `json:"content"`
}

var newSkillUploadCommand = func() (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable: %w", err)
	}
	return exec.Command(exe, "upload-skill"), nil
}

// startSkillUploadProcess passes the private task directly to a detached child
// so credentials and skill content never appear in argv or on disk.
var startSkillUploadProcess = func(task []byte) error {
	if len(task) == 0 || len(task) > maxSkillUploadTaskBytes {
		return fmt.Errorf("invalid skill upload task size")
	}
	cmd, err := newSkillUploadCommand()
	if err != nil {
		return err
	}
	cmd.SysProcAttr = drainSysProcAttr()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("open skill upload stdin: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("start skill upload: %w", err)
	}

	written := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(stdin, bytes.NewReader(task))
		written <- errors.Join(copyErr, stdin.Close())
	}()

	timer := time.NewTimer(skillUploadPipeTimeout)
	defer timer.Stop()
	select {
	case err := <-written:
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Process.Release()
			return fmt.Errorf("write skill upload task: %w", err)
		}
		return cmd.Process.Release()
	case <-timer.C:
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		<-written
		_ = cmd.Process.Release()
		return fmt.Errorf("write skill upload task: timed out")
	}
}

// startSkillContentUpload hands an upload to a detached process only when the
// final accepted ingest response requests the exact captured content.
func startSkillContentUpload(c creds, res ingestResult, skill *resolvedSkill) error {
	if !res.accepted() || res.skillCapture == nil || !res.skillCapture.contentRequired || skill == nil ||
		!skill.captureReady || res.skillCapture.rawSHA256 != skill.rawSHA256 {
		return nil
	}
	task := skillUploadTask{
		Version:   skillUploadTaskVersion,
		ServerURL: c.ServerURL,
		Project:   c.Project,
		APIKey:    c.APIKey,
		RawSHA256: skill.rawSHA256,
		Content:   skill.content,
	}
	if !validSkillUploadTask(task) {
		return fmt.Errorf("invalid skill upload task")
	}
	body, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal skill upload task: %w", err)
	}
	if len(body) > maxSkillUploadTaskBytes {
		return fmt.Errorf("marshaled skill upload task exceeds %d bytes", maxSkillUploadTaskBytes)
	}
	return startSkillUploadProcess(body)
}

// RunSkillUpload consumes one bounded detached task from stdin.
func RunSkillUpload(ctx context.Context, input io.Reader) int {
	body, err := io.ReadAll(io.LimitReader(input, maxSkillUploadTaskBytes+1))
	if err != nil || len(body) == 0 || len(body) > maxSkillUploadTaskBytes {
		return 1
	}

	var task skillUploadTask
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&task) != nil || decoder.Decode(&struct{}{}) != io.EOF || !validSkillUploadTask(task) {
		return 1
	}

	c := creds{ServerURL: task.ServerURL, APIKey: task.APIKey, Project: task.Project, Email: "", Org: "", Source: credEnv}
	if err := newClient(task.ServerURL).uploadSkillContent(ctx, c, task.RawSHA256, task.Content); err != nil {
		return 1
	}
	return 0
}

func validSkillUploadTask(task skillUploadTask) bool {
	if task.Version != skillUploadTaskVersion || task.APIKey == "" ||
		strings.ContainsAny(task.APIKey+task.Project, "\r\n") || len(task.Content) > maxSkillContentBytes || !utf8.ValidString(task.Content) {
		return false
	}
	u, err := url.Parse(task.ServerURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || insecureServerURL(task.ServerURL) {
		return false
	}
	digest := sha256.Sum256([]byte(task.Content))
	return task.RawSHA256 == hex.EncodeToString(digest[:])
}
