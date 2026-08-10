package relay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

const maxSkillUploadAPIKeyBytes = 256

type skillUploadTask struct {
	ServerURL string
	Project   string
	APIKey    string
	RawSHA256 string
	Content   string
}

func requestedSkillUploadTask(c creds, res ingestResult, skill *resolvedSkill) (skillUploadTask, bool, error) {
	if !res.accepted() || res.skillCapture == nil || !res.skillCapture.contentRequired || skill == nil ||
		!skill.captureReady || res.skillCapture.rawSHA256 != skill.rawSHA256 {
		return skillUploadTask{}, false, nil
	}
	task := skillUploadTask{
		ServerURL: c.ServerURL,
		Project:   c.Project,
		APIKey:    c.APIKey,
		RawSHA256: skill.rawSHA256,
		Content:   skill.content,
	}
	if !validSkillUploadTask(task) {
		return skillUploadTask{}, false, fmt.Errorf("invalid skill upload task")
	}
	return task, true, nil
}

func uploadSkillContent(ctx context.Context, c creds, res ingestResult, skill *resolvedSkill) error {
	task, requested, err := requestedSkillUploadTask(c, res, skill)
	if err != nil || !requested {
		return err
	}
	return executeSkillUpload(ctx, task)
}

var executeSkillUpload = func(ctx context.Context, task skillUploadTask) error {
	c := creds{ServerURL: task.ServerURL, APIKey: task.APIKey, Project: task.Project, Email: "", Org: "", Source: credEnv}
	return newClient(task.ServerURL).uploadSkillContent(ctx, c, task.RawSHA256, task.Content)
}

func validSkillUploadTask(task skillUploadTask) bool {
	if task.APIKey == "" || len(task.APIKey) > maxSkillUploadAPIKeyBytes || strings.ContainsAny(task.APIKey+task.Project, "\r\n") ||
		len(task.RawSHA256) != 64 || len(task.Content) > maxSkillContentBytes || !utf8.ValidString(task.Content) {
		return false
	}
	for _, arg := range []string{task.ServerURL, task.Project, task.RawSHA256} {
		if strings.Contains(arg, task.APIKey) {
			return false
		}
	}
	if !validRawSHA256(task.RawSHA256) {
		return false
	}
	digest := sha256.Sum256([]byte(task.Content))
	if hex.EncodeToString(digest[:]) != task.RawSHA256 {
		return false
	}
	u, err := url.Parse(task.ServerURL)
	return err == nil && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && !insecureServerURL(task.ServerURL)
}
