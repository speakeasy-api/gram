package relay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
	"github.com/speakeasy-api/gram/hooks/sdk/models/operations"
	"github.com/speakeasy-api/gram/hooks/sdk/retry"
)

const (
	skillSyncBudget          = 10 * time.Second
	skillManifestVersion     = 1
	skillManifestFilename    = ".gram-managed-skills.json"
	skillLockFilename        = ".gram-managed-skills.lock"
	skillManifestMax         = 1 << 20
	skillContextMax          = 4 << 10
	skillBodyMax             = 8 << 10
	skillReadonlyContextMax  = 24 << 10
	skillInstallPrefix       = ".gram-install-"
	skillUpdateStagedPrefix  = ".gram-update-new-"
	skillUpdateBackupPrefix  = ".gram-update-old-"
	skillRemovalBackupPrefix = ".gram-remove-old-"
	skillContextOmittedSpace = 80
)

type skillDeployment struct {
	ServerURL string `json:"server_url"`
	Org       string `json:"org"`
	Project   string `json:"project"`
}

type skillUpdatePhase string

const (
	skillUpdatePlanned     skillUpdatePhase = "planned"
	skillUpdateStaged      skillUpdatePhase = "staged"
	skillUpdateBackupMoved skillUpdatePhase = "backup_moved"
	skillUpdateInstalled   skillUpdatePhase = "installed"
)

type pendingSkillUpdate struct {
	Phase     skillUpdatePhase `json:"phase"`
	NewSHA256 string           `json:"new_sha256"`
	Staged    string           `json:"staged"`
	Backup    string           `json:"backup"`
}

type skillRemovalPhase string

const (
	skillRemovalPlanned skillRemovalPhase = "planned"
	skillRemovalMoved   skillRemovalPhase = "moved"
)

type pendingSkillRemoval struct {
	Phase  skillRemovalPhase `json:"phase"`
	Backup string            `json:"backup"`
}

type managedSkill struct {
	Deployment    skillDeployment      `json:"deployment"`
	Name          string               `json:"name"`
	Files         []string             `json:"files"`
	RawSHA256     string               `json:"raw_sha256"`
	PendingUpdate *pendingSkillUpdate  `json:"pending_update,omitempty"`
	PendingRemove *pendingSkillRemoval `json:"pending_removal,omitempty"`
}

type pendingSkillInstall struct {
	Deployment skillDeployment `json:"deployment"`
	Name       string          `json:"name"`
	Files      []string        `json:"files"`
	RawSHA256  string          `json:"raw_sha256"`
	Temporary  string          `json:"temporary"`
}

type managedSkillException struct {
	Deployment skillDeployment `json:"deployment"`
	Name       string          `json:"name"`
	Status     string          `json:"status"`
	Permanent  bool            `json:"permanent,omitempty"`
}

type skillManifest struct {
	Version         int                     `json:"version"`
	Entries         []managedSkill          `json:"entries"`
	PendingInstalls []pendingSkillInstall   `json:"pending_installs,omitempty"`
	Exceptions      []managedSkillException `json:"exceptions,omitempty"`
}

type skillSyncSnapshot struct {
	Installed  []components.SyncSkillInstalled
	Exceptions []components.SyncSkillException
}

type installedSkillNotice struct {
	Name        string
	Description string
	Path        string
}

type readonlySkillNotice struct {
	Name        string
	Description string
	Content     string
}

type skillSyncOutcome struct {
	InstalledNew     []installedSkillNotice
	Readonly         []readonlySkillNotice
	DurableChanged   bool
	PersistenceError bool
}

type manifestPersistenceError struct{ err error }

func (e *manifestPersistenceError) Error() string { return e.err.Error() }
func (e *manifestPersistenceError) Unwrap() error { return e.err }

type skillConflictError struct{ err error }

func (e *skillConflictError) Error() string { return e.err.Error() }
func (e *skillConflictError) Unwrap() error { return e.err }

type filesystemDurabilityError struct{ err error }

func (e *filesystemDurabilityError) Error() string { return e.err.Error() }
func (e *filesystemDurabilityError) Unwrap() error { return e.err }

func (r *Relay) syncSkills(ctx context.Context, skipContended bool) string {
	c, ok := resolveSkillSyncAuth(r.cfg)
	if !ok {
		return ""
	}
	root, err := claudeSkillsRoot()
	if err != nil {
		return ""
	}

	lock, contended, err := acquireSkillSyncLock(ctx, root, skipContended)
	if contended {
		return ""
	}
	if err != nil {
		if skipContended {
			return ""
		}
		return r.syncSkillsWithoutManifest(ctx, c, root)
	}
	defer func() {
		unlockFile(lock)
		_ = lock.Close()
	}()

	manifestPath := filepath.Join(root, skillManifestFilename)
	manifest, err := readSkillManifest(manifestPath)
	if err != nil {
		return r.syncSkillsWithoutManifest(ctx, c, root)
	}
	if _, err := recoverSkillManifest(root, manifestPath, &manifest); err != nil {
		return r.syncSkillsWithoutManifest(ctx, c, root)
	}

	deployment := skillDeployment{ServerURL: c.ServerURL, Org: c.Org, Project: c.Project}
	reconciled := reconcileOwnedSkills(root, deployment, &manifest)
	if reconciled {
		if err := writeSkillManifest(manifestPath, manifest); err != nil {
			return r.syncSkillsWithoutManifest(ctx, c, root)
		}
	}

	result, status := r.requestSkillSync(ctx, c, snapshotForDeployment(manifest, deployment))
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		_, _ = removeAuthorizedSkills(ctx, root, manifestPath, deployment, &manifest, r.skillSyncTransition)
		return ""
	}
	if result == nil {
		return ""
	}

	outcome := applySkillSyncResult(ctx, root, manifestPath, deployment, &manifest, result, r.skillSyncTransition)
	if outcome.DurableChanged && !outcome.PersistenceError && ctx.Err() == nil {
		_, _ = r.requestSkillSync(ctx, c, snapshotForDeployment(manifest, deployment))
	}
	if skipContended {
		return ""
	}
	return skillSyncContext(outcome)
}

func resolveSkillSyncAuth(cfg Config) (creds, bool) {
	if cfg.ConfigError != "" || insecureServerURL(cfg.ServerURL) {
		return creds{}, false
	}
	c, ok := resolveAuth(cfg)
	if !ok || (c.Source != credEnv && c.Source != credCache) || c.APIKey == strings.TrimSpace(cfg.HooksAPIKey) {
		return creds{}, false
	}
	if c.Org == "" {
		c.Org = cfg.OrgID
	}
	return c, true
}

func claudeSkillsRoot() (string, error) {
	configDir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", fmt.Errorf("resolve home directory")
		}
		configDir = filepath.Join(home, ".claude")
	}
	return filepath.Abs(filepath.Clean(filepath.Join(configDir, "skills")))
}

func acquireSkillSyncLock(ctx context.Context, root string, skipContended bool) (*os.File, bool, error) {
	info, err := os.Lstat(root)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(root, 0o755); err != nil {
			return nil, false, err
		}
	case err != nil:
		return nil, false, err
	case info.Mode()&os.ModeSymlink != 0 || !info.IsDir():
		return nil, false, fmt.Errorf("skills root is not a regular directory")
	}

	path := filepath.Join(root, skillLockFilename)
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return nil, false, fmt.Errorf("skill sync lock is irregular")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	pathInfo, pathErr := os.Lstat(path)
	openInfo, openErr := f.Stat()
	if pathErr != nil || openErr != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() || !os.SameFile(pathInfo, openInfo) {
		_ = f.Close()
		return nil, false, fmt.Errorf("skill sync lock changed while opening")
	}
	for {
		locked, err := tryLockFile(f)
		if err != nil {
			_ = f.Close()
			return nil, false, err
		}
		if locked {
			return f, false, nil
		}
		if skipContended {
			_ = f.Close()
			return nil, true, nil
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			_ = f.Close()
			return nil, true, nil
		case <-timer.C:
		}
	}
}

func emptySkillManifest() skillManifest {
	return skillManifest{
		Version:         skillManifestVersion,
		Entries:         []managedSkill{},
		PendingInstalls: []pendingSkillInstall{},
		Exceptions:      []managedSkillException{},
	}
}

func readSkillManifest(path string) (skillManifest, error) {
	manifest := emptySkillManifest()
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return manifest, nil
	}
	if err != nil {
		return skillManifest{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > skillManifestMax {
		return skillManifest{}, fmt.Errorf("skill manifest is irregular or too large")
	}
	f, err := os.Open(path)
	if err != nil {
		return skillManifest{}, err
	}
	defer func() { _ = f.Close() }()
	decoder := json.NewDecoder(f)
	if err := decoder.Decode(&manifest); err != nil {
		return skillManifest{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return skillManifest{}, fmt.Errorf("skill manifest has trailing JSON")
		}
		return skillManifest{}, fmt.Errorf("read skill manifest trailing data: %w", err)
	}
	if manifest.Version != skillManifestVersion || !validSkillManifest(manifest) {
		return skillManifest{}, fmt.Errorf("invalid skill manifest")
	}
	return manifest, nil
}

func validSkillManifest(manifest skillManifest) bool {
	names := make(map[string]struct{}, len(manifest.Entries))
	stateNames := map[string]struct{}{}
	for _, entry := range manifest.Entries {
		if !validOwnedSkill(entry.Name, entry.Files, entry.RawSHA256) || (entry.PendingUpdate != nil && entry.PendingRemove != nil) {
			return false
		}
		if entry.PendingUpdate != nil {
			pending := entry.PendingUpdate
			if !validSkillUpdatePhase(pending.Phase) || !validSkillHash(pending.NewSHA256) || !validStateName(pending.Staged, skillUpdateStagedPrefix) || !validStateName(pending.Backup, skillUpdateBackupPrefix) {
				return false
			}
			for _, stateName := range []string{pending.Staged, pending.Backup} {
				if _, exists := stateNames[stateName]; exists {
					return false
				}
				stateNames[stateName] = struct{}{}
			}
		}
		if entry.PendingRemove != nil {
			pending := entry.PendingRemove
			if !validSkillRemovalPhase(pending.Phase) || !validStateName(pending.Backup, skillRemovalBackupPrefix) {
				return false
			}
			if _, exists := stateNames[pending.Backup]; exists {
				return false
			}
			stateNames[pending.Backup] = struct{}{}
		}
		if _, exists := names[entry.Name]; exists {
			return false
		}
		names[entry.Name] = struct{}{}
	}
	for _, install := range manifest.PendingInstalls {
		if !validOwnedSkill(install.Name, install.Files, install.RawSHA256) || !validStateName(install.Temporary, skillInstallPrefix) {
			return false
		}
		if _, exists := names[install.Name]; exists {
			return false
		}
		names[install.Name] = struct{}{}
		if _, exists := stateNames[install.Temporary]; exists {
			return false
		}
		stateNames[install.Temporary] = struct{}{}
	}
	exceptions := map[string]struct{}{}
	for _, exception := range manifest.Exceptions {
		if !validSkillName(exception.Name) || (exception.Status != string(components.StatusConflictSkipped) && exception.Status != string(components.StatusFsReadonly)) {
			return false
		}
		key := deploymentKey(exception.Deployment) + "\x00" + exception.Name
		if _, exists := exceptions[key]; exists {
			return false
		}
		exceptions[key] = struct{}{}
	}
	return true
}

func validSkillUpdatePhase(phase skillUpdatePhase) bool {
	return phase == skillUpdatePlanned || phase == skillUpdateStaged || phase == skillUpdateBackupMoved || phase == skillUpdateInstalled
}

func validSkillRemovalPhase(phase skillRemovalPhase) bool {
	return phase == skillRemovalPlanned || phase == skillRemovalMoved
}

func validOwnedSkill(name string, files []string, hash string) bool {
	return validSkillName(name) && validSkillHash(hash) && len(files) == 1 && files[0] == filepath.Join(name, "SKILL.md")
}

func validStateName(name, prefix string) bool {
	return name == filepath.Base(name) && strings.HasPrefix(name, prefix) && len(name) > len(prefix) && !strings.ContainsAny(name, `/\`)
}

func writeSkillManifest(path string, manifest skillManifest) error {
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return fmt.Errorf("skill manifest is irregular")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	sort.Slice(manifest.Entries, func(i, j int) bool { return manifest.Entries[i].Name < manifest.Entries[j].Name })
	sort.Slice(manifest.PendingInstalls, func(i, j int) bool {
		return deploymentKey(manifest.PendingInstalls[i].Deployment)+"\x00"+manifest.PendingInstalls[i].Name < deploymentKey(manifest.PendingInstalls[j].Deployment)+"\x00"+manifest.PendingInstalls[j].Name
	})
	sort.Slice(manifest.Exceptions, func(i, j int) bool {
		return deploymentKey(manifest.Exceptions[i].Deployment)+"\x00"+manifest.Exceptions[i].Name < deploymentKey(manifest.Exceptions[j].Deployment)+"\x00"+manifest.Exceptions[j].Name
	})
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if len(body) > skillManifestMax {
		return fmt.Errorf("skill manifest exceeds 1 MiB")
	}
	return atomicWriteFile(path, body, 0o600)
}

func atomicWriteFile(path string, body []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	dirInfo, err := os.Lstat(dir)
	if err != nil || dirInfo.Mode()&os.ModeSymlink != 0 || !dirInfo.IsDir() {
		return fmt.Errorf("write directory is irregular")
	}
	tmp, err := os.CreateTemp(dir, ".gram-skill-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	if err := syncDirectory(dir); err != nil {
		return &filesystemDurabilityError{err: err}
	}
	return nil
}

func recoverSkillManifest(root, manifestPath string, manifest *skillManifest) (bool, error) {
	changed := false
	for i := len(manifest.PendingInstalls) - 1; i >= 0; i-- {
		install := manifest.PendingInstalls[i]
		finalPath := filepath.Join(root, install.Name)
		tempPath := filepath.Join(root, install.Temporary)
		finalExists, finalExact := exactSkillDirectory(finalPath, install.RawSHA256)
		tempExists, tempExact := exactSkillDirectory(tempPath, install.RawSHA256)
		switch {
		case finalExact && !tempExists:
			manifest.Entries = append(manifest.Entries, managedSkill{Deployment: install.Deployment, Name: install.Name, Files: slices.Clone(install.Files), RawSHA256: install.RawSHA256, PendingUpdate: nil, PendingRemove: nil})
		case tempExact && !finalExists:
			if err := renameNoReplace(tempPath, finalPath); err != nil {
				if pathExists(finalPath) {
					setSkillException(manifest, install.Deployment, install.Name, string(components.StatusConflictSkipped), false)
					break
				}
				return changed, err
			}
			if err := syncDirectory(root); err != nil {
				return changed, &manifestPersistenceError{err: &filesystemDurabilityError{err: err}}
			}
			manifest.Entries = append(manifest.Entries, managedSkill{Deployment: install.Deployment, Name: install.Name, Files: slices.Clone(install.Files), RawSHA256: install.RawSHA256, PendingUpdate: nil, PendingRemove: nil})
		case !finalExists && !tempExists:
		case finalExists:
			setSkillException(manifest, install.Deployment, install.Name, string(components.StatusConflictSkipped), false)
		default:
			// An irregular staged path is left untouched and no longer owned.
		}
		manifest.PendingInstalls = append(manifest.PendingInstalls[:i], manifest.PendingInstalls[i+1:]...)
		changed = true
	}

	if changed {
		if err := writeSkillManifest(manifestPath, *manifest); err != nil {
			return changed, &manifestPersistenceError{err: err}
		}
	}

	for i := len(manifest.Entries) - 1; i >= 0; i-- {
		entry := manifest.Entries[i]
		if entry.PendingUpdate != nil {
			if _, err := resumePendingUpdate(root, manifestPath, manifest, i, nil, nil); err != nil {
				var conflictErr *skillConflictError
				if !errors.As(err, &conflictErr) {
					return changed, err
				}
			}
			changed = true
			continue
		}
		if entry.PendingRemove == nil {
			continue
		}
		if _, err := resumePendingRemoval(root, manifestPath, manifest, i, nil, nil); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

func reconcileOwnedSkills(root string, deployment skillDeployment, manifest *skillManifest) bool {
	changed := false
	kept := manifest.Entries[:0]
	for _, entry := range manifest.Entries {
		if _, intact := intactManagedSkill(root, entry); intact {
			kept = append(kept, entry)
			continue
		}
		setSkillException(manifest, entry.Deployment, entry.Name, string(components.StatusConflictSkipped), true)
		changed = true
	}
	manifest.Entries = kept
	for i := len(manifest.Exceptions) - 1; i >= 0; i-- {
		exception := manifest.Exceptions[i]
		if exception.Deployment != deployment || !exception.Permanent || ownEntryIndex(*manifest, deployment, exception.Name) >= 0 {
			continue
		}
		_, err := os.Lstat(filepath.Join(root, exception.Name))
		if errors.Is(err, os.ErrNotExist) {
			manifest.Exceptions = append(manifest.Exceptions[:i], manifest.Exceptions[i+1:]...)
			changed = true
		}
	}
	return changed
}

func intactManagedSkill(root string, entry managedSkill) ([]byte, bool) {
	hash, ok := managedSkillHash(root, entry)
	if !ok || hash != entry.RawSHA256 {
		return nil, false
	}
	body, err := os.ReadFile(filepath.Join(root, entry.Files[0]))
	return body, err == nil
}

func managedSkillHash(root string, entry managedSkill) (string, bool) {
	if !validOwnedSkill(entry.Name, entry.Files, entry.RawSHA256) {
		return "", false
	}
	dir := filepath.Join(root, entry.Name)
	dirInfo, err := os.Lstat(dir)
	if err != nil || dirInfo.Mode()&os.ModeSymlink != 0 || !dirInfo.IsDir() {
		return "", false
	}
	path := filepath.Join(root, entry.Files[0])
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return rawSkillHash(body), true
}

func exactSkillDirectory(path, hash string) (bool, bool) {
	exists, exact, _ := exactSkillDirectoryState(path, hash)
	return exists, exact
}

func exactSkillDirectoryState(path, hash string) (bool, bool, os.FileInfo) {
	dirInfo, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil || dirInfo.Mode()&os.ModeSymlink != 0 || !dirInfo.IsDir() {
		return true, false, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 1 || entries[0].Name() != "SKILL.md" || entries[0].Type()&os.ModeSymlink != 0 {
		return true, false, dirInfo
	}
	skillHash, skillRegular := regularSkillFileHash(filepath.Join(path, "SKILL.md"))
	currentDirInfo, err := os.Lstat(path)
	if err != nil || currentDirInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(dirInfo, currentDirInfo) {
		return true, false, nil
	}
	return true, skillRegular && skillHash == hash, currentDirInfo
}

func removeExactSkillDirectory(path, hash string) error {
	_, exact := exactSkillDirectory(path, hash)
	if !exact {
		return fmt.Errorf("skill directory is not exactly owned")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("skill directory changed before removal")
	}
	if err := os.Remove(filepath.Join(path, "SKILL.md")); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return &filesystemDurabilityError{err: err}
	}
	return nil
}

func snapshotForDeployment(manifest skillManifest, deployment skillDeployment) skillSyncSnapshot {
	snapshot := skillSyncSnapshot{Installed: []components.SyncSkillInstalled{}, Exceptions: []components.SyncSkillException{}}
	exceptionNames := map[string]struct{}{}
	for _, exception := range manifest.Exceptions {
		if exception.Deployment == deployment {
			snapshot.Exceptions = append(snapshot.Exceptions, components.SyncSkillException{Name: exception.Name, Status: components.Status(exception.Status)})
			exceptionNames[exception.Name] = struct{}{}
		}
	}
	for _, entry := range manifest.Entries {
		if entry.Deployment != deployment || entry.PendingUpdate != nil || entry.PendingRemove != nil {
			continue
		}
		if _, excepted := exceptionNames[entry.Name]; excepted {
			continue
		}
		snapshot.Installed = append(snapshot.Installed, components.SyncSkillInstalled{Name: entry.Name, RawSha256: entry.RawSHA256})
	}
	sort.Slice(snapshot.Installed, func(i, j int) bool { return snapshot.Installed[i].Name < snapshot.Installed[j].Name })
	sort.Slice(snapshot.Exceptions, func(i, j int) bool { return snapshot.Exceptions[i].Name < snapshot.Exceptions[j].Name })
	return snapshot
}

func (r *Relay) requestSkillSync(ctx context.Context, c creds, snapshot skillSyncSnapshot) (*components.SyncSkillsResult, int) {
	req := operations.SyncSkillsRequest{
		GramKey:           nil,
		GramProject:       nil,
		XGramHookHostname: syncHostname(),
		IdempotencyKey:    new(newIdempotencyToken()),
		Body: components.SyncSkillsRequestBody{
			Exceptions: snapshot.Exceptions,
			Installed:  snapshot.Installed,
			Provider:   components.ProviderClaude,
		},
	}
	security := &operations.SyncSkillsSecurity{ApikeyHeaderGramKey: new(c.APIKey), ProjectSlugHeaderGramProject: nil}
	if c.Project != "" {
		security.ProjectSlugHeaderGramProject = new(c.Project)
	}
	res, err := r.client.sdk.Skills.Sync(ctx, req, security, operations.WithRetries(retry.Config{Strategy: "none", Backoff: nil, RetryConnectionErrors: false}))
	if err != nil {
		return nil, interpretError(err).statusCode
	}
	if res == nil {
		return nil, 0
	}
	return res.SyncSkillsResult, res.StatusCode
}

func syncHostname() string {
	value := strings.TrimSpace(hostname())
	if value == "" {
		return "unknown"
	}
	return truncateUTF8(value, 255)
}

func (r *Relay) syncSkillsWithoutManifest(ctx context.Context, c creds, root string) string {
	result, status := r.requestSkillSync(ctx, c, skillSyncSnapshot{Installed: []components.SyncSkillInstalled{}, Exceptions: []components.SyncSkillException{}})
	if result == nil || status < 200 || status >= 300 {
		return ""
	}
	exceptions := make([]components.SyncSkillException, 0, len(result.Updates))
	notices := make([]readonlySkillNotice, 0, len(result.Updates))
	for _, update := range result.Updates {
		if !validSkillUpdate(update) {
			continue
		}
		status := components.StatusConflictSkipped
		_, err := os.Lstat(filepath.Join(root, update.Name))
		if errors.Is(err, os.ErrNotExist) || err != nil {
			status = components.StatusFsReadonly
			notices = append(notices, readonlySkillNotice{Name: update.Name, Description: strDeref(update.Description), Content: update.Content})
		}
		exceptions = append(exceptions, components.SyncSkillException{Name: update.Name, Status: status})
	}
	if len(exceptions) > 0 && ctx.Err() == nil {
		_, _ = r.requestSkillSync(ctx, c, skillSyncSnapshot{Installed: []components.SyncSkillInstalled{}, Exceptions: exceptions})
	}
	return skillSyncContext(skillSyncOutcome{InstalledNew: nil, Readonly: notices, DurableChanged: false, PersistenceError: true})
}

func applySkillSyncResult(ctx context.Context, root, manifestPath string, deployment skillDeployment, manifest *skillManifest, result *components.SyncSkillsResult, transition func(string, string)) skillSyncOutcome {
	outcome := skillSyncOutcome{InstalledNew: []installedSkillNotice{}, Readonly: []readonlySkillNotice{}, DurableChanged: false, PersistenceError: false}
	updates := make(map[string]components.SyncSkillUpdate, len(result.Updates))
	invalidNames := map[string]struct{}{}
	for _, update := range result.Updates {
		if !validSkillUpdate(update) {
			continue
		}
		if _, duplicate := updates[update.Name]; duplicate {
			delete(updates, update.Name)
			invalidNames[update.Name] = struct{}{}
			continue
		}
		if _, invalid := invalidNames[update.Name]; !invalid {
			updates[update.Name] = update
		}
	}

	removals := slices.Compact(slices.Sorted(slices.Values(result.Removals)))
	removalConflicts := make(map[string]struct{})
	for _, name := range removals {
		if ctx.Err() != nil {
			break
		}
		if !validSkillName(name) {
			continue
		}
		if _, collision := updates[name]; collision {
			continue
		}
		entryIndex := ownEntryIndex(*manifest, deployment, name)
		if entryIndex < 0 {
			continue
		}
		removed, err := removeManagedSkill(root, manifestPath, manifest, entryIndex, transition)
		if err != nil {
			var persistenceErr *manifestPersistenceError
			if errors.As(err, &persistenceErr) {
				outcome.PersistenceError = true
				return outcome
			}
			continue
		}
		outcome.DurableChanged = outcome.DurableChanged || removed
		if permanentSkillConflict(*manifest, deployment, name) {
			removalConflicts[name] = struct{}{}
		}
	}

	names := make([]string, 0, len(updates))
	for name := range updates {
		names = append(names, name)
	}
	slices.Sort(names)
	desired := make(map[string]struct{}, len(names))
	metadataBefore := cloneManifest(*manifest)
	metadataChanged := false
	candidates := make([]components.SyncSkillUpdate, 0, len(names))
	for _, name := range names {
		desired[name] = struct{}{}
		update := updates[name]
		if permanentSkillConflict(*manifest, deployment, name) || otherDeploymentOwns(*manifest, deployment, name) {
			metadataChanged = setSkillException(manifest, deployment, name, string(components.StatusConflictSkipped), permanentSkillConflict(*manifest, deployment, name)) || metadataChanged
			continue
		}
		entryIndex := ownEntryIndex(*manifest, deployment, name)
		if entryIndex < 0 {
			if _, err := os.Lstat(filepath.Join(root, name)); err == nil || !errors.Is(err, os.ErrNotExist) {
				metadataChanged = setSkillException(manifest, deployment, name, string(components.StatusConflictSkipped), false) || metadataChanged
				continue
			}
		} else if _, intact := intactManagedSkill(root, manifest.Entries[entryIndex]); !intact {
			manifest.Entries = append(manifest.Entries[:entryIndex], manifest.Entries[entryIndex+1:]...)
			setSkillException(manifest, deployment, name, string(components.StatusConflictSkipped), true)
			metadataChanged = true
			continue
		}
		candidates = append(candidates, update)
	}
	for i := len(manifest.Exceptions) - 1; i >= 0; i-- {
		exception := manifest.Exceptions[i]
		if exception.Deployment == deployment {
			if _, stillDesired := desired[exception.Name]; !stillDesired {
				if _, removalConflict := removalConflicts[exception.Name]; removalConflict {
					continue
				}
				manifest.Exceptions = append(manifest.Exceptions[:i], manifest.Exceptions[i+1:]...)
				metadataChanged = true
			}
		}
	}
	if metadataChanged {
		if err := writeSkillManifest(manifestPath, *manifest); err != nil {
			*manifest = metadataBefore
			outcome.PersistenceError = true
			return outcome
		}
		outcome.DurableChanged = true
	}

	metadataChanged = false
	for _, update := range candidates {
		if ctx.Err() != nil {
			break
		}
		if metadataChanged {
			if err := writeSkillManifest(manifestPath, *manifest); err != nil {
				outcome.PersistenceError = true
				return outcome
			}
			outcome.DurableChanged = true
			metadataChanged = false
		}
		entryIndex := ownEntryIndex(*manifest, deployment, update.Name)
		if entryIndex < 0 {
			installed, err := installManagedSkill(root, manifestPath, deployment, manifest, update)
			if err == nil && installed {
				outcome.DurableChanged = true
				outcome.InstalledNew = append(outcome.InstalledNew, installedSkillNotice{Name: update.Name, Description: strDeref(update.Description), Path: filepath.Join(root, update.Name, "SKILL.md")})
				continue
			}
			var persistenceErr *manifestPersistenceError
			if errors.As(err, &persistenceErr) {
				outcome.PersistenceError = true
				return outcome
			}
			var conflictErr *skillConflictError
			if errors.As(err, &conflictErr) {
				metadataChanged = setSkillException(manifest, deployment, update.Name, string(components.StatusConflictSkipped), false) || metadataChanged
				continue
			}
		} else {
			updated, err := updateManagedSkill(root, manifestPath, manifest, entryIndex, update, transition)
			if err == nil && updated {
				outcome.DurableChanged = true
				continue
			}
			var persistenceErr *manifestPersistenceError
			if errors.As(err, &persistenceErr) {
				outcome.PersistenceError = true
				return outcome
			}
			var conflictErr *skillConflictError
			if errors.As(err, &conflictErr) {
				entryIndex = ownEntryIndex(*manifest, deployment, update.Name)
				if entryIndex >= 0 {
					manifest.Entries = append(manifest.Entries[:entryIndex], manifest.Entries[entryIndex+1:]...)
				}
				setSkillException(manifest, deployment, update.Name, string(components.StatusConflictSkipped), true)
				metadataChanged = true
				continue
			}
		}
		metadataChanged = setSkillException(manifest, deployment, update.Name, string(components.StatusFsReadonly), false) || metadataChanged
		outcome.Readonly = append(outcome.Readonly, readonlySkillNotice{Name: update.Name, Description: strDeref(update.Description), Content: update.Content})
	}
	if metadataChanged {
		if err := writeSkillManifest(manifestPath, *manifest); err != nil {
			outcome.PersistenceError = true
			return outcome
		}
		outcome.DurableChanged = true
	}
	return outcome
}

func installManagedSkill(root, manifestPath string, deployment skillDeployment, manifest *skillManifest, update components.SyncSkillUpdate) (bool, error) {
	tempPath, err := os.MkdirTemp(root, skillInstallPrefix)
	if err != nil {
		return false, err
	}
	tempName := filepath.Base(tempPath)
	cleanup := true
	defer func() {
		if cleanup {
			_ = removeExactSkillDirectory(tempPath, update.RawSha256)
			_ = os.Remove(tempPath)
		}
	}()
	if err := atomicWriteFile(filepath.Join(tempPath, "SKILL.md"), []byte(update.Content), 0o644); err != nil {
		return false, err
	}
	install := pendingSkillInstall{Deployment: deployment, Name: update.Name, Files: []string{filepath.Join(update.Name, "SKILL.md")}, RawSHA256: update.RawSha256, Temporary: tempName}
	manifest.PendingInstalls = append(manifest.PendingInstalls, install)
	if err := writeSkillManifest(manifestPath, *manifest); err != nil {
		manifest.PendingInstalls = manifest.PendingInstalls[:len(manifest.PendingInstalls)-1]
		return false, &manifestPersistenceError{err: err}
	}
	cleanup = false
	finalPath := filepath.Join(root, update.Name)
	if err := renameNoReplace(tempPath, finalPath); err != nil {
		manifest.PendingInstalls = manifest.PendingInstalls[:len(manifest.PendingInstalls)-1]
		if writeErr := writeSkillManifest(manifestPath, *manifest); writeErr != nil {
			return false, &manifestPersistenceError{err: writeErr}
		}
		cleanup = true
		if pathExists(finalPath) {
			return false, &skillConflictError{err: err}
		}
		return false, err
	}
	if err := syncDirectory(root); err != nil {
		return false, &manifestPersistenceError{err: &filesystemDurabilityError{err: err}}
	}
	pendingState := cloneManifest(*manifest)
	removePendingInstall(manifest, deployment, update.Name)
	manifest.Entries = append(manifest.Entries, managedSkill{Deployment: deployment, Name: update.Name, Files: slices.Clone(install.Files), RawSHA256: update.RawSha256, PendingUpdate: nil, PendingRemove: nil})
	clearSkillException(manifest, deployment, update.Name)
	if err := writeSkillManifest(manifestPath, *manifest); err != nil {
		*manifest = pendingState
		return false, &manifestPersistenceError{err: err}
	}
	return true, nil
}

type skillUpdateIdentity struct {
	final  os.FileInfo
	staged os.FileInfo
}

func updateManagedSkill(root, manifestPath string, manifest *skillManifest, entryIndex int, update components.SyncSkillUpdate, transition func(string, string)) (bool, error) {
	entry := manifest.Entries[entryIndex]
	if entry.RawSHA256 == update.RawSha256 {
		return false, nil
	}
	dir := filepath.Join(root, entry.Name)
	finalPath := filepath.Join(root, entry.Files[0])
	oldHash, finalInfo, finalExact := regularSkillFileState(finalPath)
	if !finalExact || oldHash != entry.RawSHA256 {
		return false, &skillConflictError{err: fmt.Errorf("managed skill changed before update")}
	}
	token := newIdempotencyToken()
	pending := &pendingSkillUpdate{
		Phase:     skillUpdatePlanned,
		NewSHA256: update.RawSha256,
		Staged:    skillUpdateStagedPrefix + token,
		Backup:    skillUpdateBackupPrefix + token,
	}
	stagedPath := filepath.Join(dir, pending.Staged)
	backupPath := filepath.Join(dir, pending.Backup)
	if pathExists(stagedPath) || pathExists(backupPath) {
		return false, fmt.Errorf("update state path already exists")
	}
	entry.PendingUpdate = pending
	if err := persistSkillEntry(manifestPath, manifest, entryIndex, entry); err != nil {
		return false, err
	}
	if err := writeNewSkillFile(stagedPath, []byte(update.Content), 0o644); err != nil {
		return false, &manifestPersistenceError{err: err}
	}
	stagedHash, stagedInfo, stagedExact := regularSkillFileState(stagedPath)
	if !stagedExact || stagedHash != update.RawSha256 {
		return false, &manifestPersistenceError{err: fmt.Errorf("staged update changed after write")}
	}
	entry = manifest.Entries[entryIndex]
	entry = withSkillUpdatePhase(entry, skillUpdateStaged)
	if err := persistSkillEntry(manifestPath, manifest, entryIndex, entry); err != nil {
		return false, err
	}
	return resumePendingUpdate(root, manifestPath, manifest, entryIndex, &skillUpdateIdentity{final: finalInfo, staged: stagedInfo}, transition)
}

func resumePendingUpdate(root, manifestPath string, manifest *skillManifest, entryIndex int, identity *skillUpdateIdentity, transition func(string, string)) (bool, error) {
	for {
		entry := manifest.Entries[entryIndex]
		pending := entry.PendingUpdate
		if pending == nil {
			return true, nil
		}
		dir := filepath.Join(root, entry.Name)
		finalPath := filepath.Join(root, entry.Files[0])
		stagedPath := filepath.Join(dir, pending.Staged)
		backupPath := filepath.Join(dir, pending.Backup)
		finalExists, finalOld, finalInfo := exactSkillFile(finalPath, entry.RawSHA256)
		_, finalNew, _ := exactSkillFile(finalPath, pending.NewSHA256)
		stagedExists, stagedExact, stagedInfo := exactSkillFile(stagedPath, pending.NewSHA256)
		backupExists, backupExact, _ := exactSkillFile(backupPath, entry.RawSHA256)
		if identity == nil && (pending.Phase == skillUpdatePlanned || pending.Phase == skillUpdateStaged) {
			if backupExists && !backupExact {
				return false, irregularSkillStateError(backupPath)
			}
			if !backupExact {
				if finalOld {
					if stagedExists && !stagedExact {
						return false, irregularSkillStateError(stagedPath)
					}
					if stagedExact {
						if err := removeExactSkillFile(stagedPath, pending.NewSHA256); err != nil {
							return false, &manifestPersistenceError{err: err}
						}
					}
					if err := syncSkillStateDirectory(root, dir); err != nil {
						return false, &manifestPersistenceError{err: &filesystemDurabilityError{err: err}}
					}
					entry.PendingUpdate = nil
					if err := persistSkillEntry(manifestPath, manifest, entryIndex, entry); err != nil {
						return false, err
					}
					return true, nil
				}
				return resolveUpdateConflict(root, manifestPath, manifest, entryIndex)
			}
			if finalExists {
				return resolveUpdateConflict(root, manifestPath, manifest, entryIndex)
			}
			if stagedExists && !stagedExact {
				return false, irregularSkillStateError(stagedPath)
			}
			if !stagedExact {
				return resolveUpdateConflict(root, manifestPath, manifest, entryIndex)
			}
			entry = withSkillUpdatePhase(entry, skillUpdateBackupMoved)
			if err := persistSkillEntry(manifestPath, manifest, entryIndex, entry); err != nil {
				return false, err
			}
			continue
		}

		switch pending.Phase {
		case skillUpdatePlanned:
			if finalNew {
				return resolveUpdateConflict(root, manifestPath, manifest, entryIndex)
			}
			if finalOld && stagedExact && !backupExists {
				entry = withSkillUpdatePhase(entry, skillUpdateStaged)
				if err := persistSkillEntry(manifestPath, manifest, entryIndex, entry); err != nil {
					return false, err
				}
				continue
			}
			if finalOld && !stagedExists && !backupExists {
				entry.PendingUpdate = nil
				if err := persistSkillEntry(manifestPath, manifest, entryIndex, entry); err != nil {
					return false, err
				}
				return true, nil
			}
			return resolveUpdateConflict(root, manifestPath, manifest, entryIndex)

		case skillUpdateStaged:
			if finalNew {
				return resolveUpdateConflict(root, manifestPath, manifest, entryIndex)
			}
			if !finalExists && backupExact && stagedExact {
				entry = withSkillUpdatePhase(entry, skillUpdateBackupMoved)
				if err := persistSkillEntry(manifestPath, manifest, entryIndex, entry); err != nil {
					return false, err
				}
				continue
			}
			if !finalOld || !stagedExact || backupExists || identity != nil && (!os.SameFile(identity.final, finalInfo) || !os.SameFile(identity.staged, stagedInfo)) {
				return resolveUpdateConflict(root, manifestPath, manifest, entryIndex)
			}
			if transition != nil {
				transition("update-before-backup", entry.Name)
				finalExists, finalOld, finalInfo = exactSkillFile(finalPath, entry.RawSHA256)
				_, finalNew, _ = exactSkillFile(finalPath, pending.NewSHA256)
				stagedExists, stagedExact, stagedInfo = exactSkillFile(stagedPath, pending.NewSHA256)
				if finalNew || !finalOld || !stagedExact || pathExists(backupPath) || identity != nil && (!os.SameFile(identity.final, finalInfo) || !os.SameFile(identity.staged, stagedInfo)) {
					return resolveUpdateConflict(root, manifestPath, manifest, entryIndex)
				}
			}
			if err := renameNoReplace(finalPath, backupPath); err != nil {
				return false, &manifestPersistenceError{err: err}
			}
			if err := syncSkillStateDirectory(root, dir); err != nil {
				return false, &manifestPersistenceError{err: &filesystemDurabilityError{err: err}}
			}
			backupHash, backupInfo, backupRegular := regularSkillFileState(backupPath)
			if !backupRegular || backupHash != entry.RawSHA256 || !os.SameFile(finalInfo, backupInfo) {
				return resolveUpdateConflict(root, manifestPath, manifest, entryIndex)
			}
			entry = withSkillUpdatePhase(entry, skillUpdateBackupMoved)
			if err := persistSkillEntry(manifestPath, manifest, entryIndex, entry); err != nil {
				return false, err
			}

		case skillUpdateBackupMoved:
			if finalNew && !stagedExists && backupExact {
				entry = withSkillUpdatePhase(entry, skillUpdateInstalled)
				if err := persistSkillEntry(manifestPath, manifest, entryIndex, entry); err != nil {
					return false, err
				}
				continue
			}
			if finalExists || !backupExact || !stagedExact {
				return resolveUpdateConflict(root, manifestPath, manifest, entryIndex)
			}
			if transition != nil {
				transition("update-before-install", entry.Name)
				finalExists, _, _ = exactSkillFile(finalPath, pending.NewSHA256)
				stagedExists, stagedExact, stagedInfo = exactSkillFile(stagedPath, pending.NewSHA256)
				_, backupExact, _ = exactSkillFile(backupPath, entry.RawSHA256)
				if finalExists || !backupExact || !stagedExact || identity != nil && !os.SameFile(identity.staged, stagedInfo) {
					return resolveUpdateConflict(root, manifestPath, manifest, entryIndex)
				}
			}
			if err := renameNoReplace(stagedPath, finalPath); err != nil {
				return false, &manifestPersistenceError{err: err}
			}
			if err := syncSkillStateDirectory(root, dir); err != nil {
				return false, &manifestPersistenceError{err: &filesystemDurabilityError{err: err}}
			}
			installedHash, installedInfo, installedRegular := regularSkillFileState(finalPath)
			if !installedRegular || installedHash != pending.NewSHA256 || !os.SameFile(stagedInfo, installedInfo) {
				return resolveUpdateConflict(root, manifestPath, manifest, entryIndex)
			}
			entry = withSkillUpdatePhase(entry, skillUpdateInstalled)
			if err := persistSkillEntry(manifestPath, manifest, entryIndex, entry); err != nil {
				return false, err
			}

		case skillUpdateInstalled:
			if !finalNew || stagedExists || backupExists && !backupExact {
				return resolveUpdateConflict(root, manifestPath, manifest, entryIndex)
			}
			if backupExact {
				if err := removeExactSkillFile(backupPath, entry.RawSHA256); err != nil {
					return false, &manifestPersistenceError{err: err}
				}
			}
			if err := syncSkillStateDirectory(root, dir); err != nil {
				return false, &manifestPersistenceError{err: &filesystemDurabilityError{err: err}}
			}
			if transition != nil {
				transition("update-before-finalize", entry.Name)
			}
			finalHash, finalInfo, finalRegular := regularSkillFileState(finalPath)
			if !finalRegular || finalHash != pending.NewSHA256 || identity != nil && !os.SameFile(identity.staged, finalInfo) {
				return resolveUpdateConflict(root, manifestPath, manifest, entryIndex)
			}
			before := cloneManifest(*manifest)
			entry.RawSHA256 = pending.NewSHA256
			entry.PendingUpdate = nil
			manifest.Entries[entryIndex] = entry
			clearSkillException(manifest, entry.Deployment, entry.Name)
			if err := writeSkillManifest(manifestPath, *manifest); err != nil {
				*manifest = before
				return false, &manifestPersistenceError{err: err}
			}
			return true, nil
		}
	}
}

func resolveUpdateConflict(root, manifestPath string, manifest *skillManifest, entryIndex int) (bool, error) {
	entry := manifest.Entries[entryIndex]
	pending := entry.PendingUpdate
	dir := filepath.Join(root, entry.Name)
	finalPath := filepath.Join(root, entry.Files[0])
	stagedPath := filepath.Join(dir, pending.Staged)
	backupPath := filepath.Join(dir, pending.Backup)
	stagedExists, stagedExact, _ := exactSkillFile(stagedPath, pending.NewSHA256)
	backupExists, backupExact, _ := exactSkillFile(backupPath, entry.RawSHA256)
	if stagedExists && !stagedExact {
		return false, irregularSkillStateError(stagedPath)
	}
	if backupExists && !backupExact {
		return false, irregularSkillStateError(backupPath)
	}
	if !pathExists(finalPath) && backupExact {
		if err := renameNoReplace(backupPath, finalPath); err != nil {
			return false, &manifestPersistenceError{err: err}
		}
		if err := syncDirectory(dir); err != nil {
			return false, &manifestPersistenceError{err: &filesystemDurabilityError{err: err}}
		}
	}
	if stagedExact {
		if err := removeExactSkillFile(stagedPath, pending.NewSHA256); err != nil {
			return false, &manifestPersistenceError{err: err}
		}
	}
	if backupExact && pathExists(backupPath) {
		if err := removeExactSkillFile(backupPath, entry.RawSHA256); err != nil {
			return false, &manifestPersistenceError{err: err}
		}
	}
	if err := syncSkillStateDirectory(root, dir); err != nil {
		return false, &manifestPersistenceError{err: &filesystemDurabilityError{err: err}}
	}
	if err := relinquishManagedSkill(manifestPath, manifest, entryIndex, true); err != nil {
		return false, err
	}
	return true, &skillConflictError{err: fmt.Errorf("managed skill changed during update")}
}

func irregularSkillStateError(path string) error {
	return &manifestPersistenceError{err: fmt.Errorf("transaction state path is not exact: %s", filepath.Base(path))}
}

func exactSkillFile(path, hash string) (bool, bool, os.FileInfo) {
	actual, info, regular := regularSkillFileState(path)
	if regular {
		return true, actual == hash, info
	}
	return pathExists(path), false, nil
}

func persistSkillEntry(manifestPath string, manifest *skillManifest, entryIndex int, entry managedSkill) error {
	previous := cloneManifest(*manifest)
	manifest.Entries[entryIndex] = entry
	if err := writeSkillManifest(manifestPath, *manifest); err != nil {
		*manifest = previous
		return &manifestPersistenceError{err: err}
	}
	return nil
}

func withSkillUpdatePhase(entry managedSkill, phase skillUpdatePhase) managedSkill {
	pending := *entry.PendingUpdate
	pending.Phase = phase
	entry.PendingUpdate = &pending
	return entry
}

func withSkillRemovalPhase(entry managedSkill, phase skillRemovalPhase) managedSkill {
	pending := *entry.PendingRemove
	pending.Phase = phase
	entry.PendingRemove = &pending
	return entry
}

func writeNewSkillFile(path string, body []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	info, err := os.Lstat(dir)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("write directory is irregular")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := syncDirectory(dir); err != nil {
		return &filesystemDurabilityError{err: err}
	}
	return nil
}

func regularSkillFileHash(path string) (string, bool) {
	hash, _, ok := regularSkillFileState(path)
	return hash, ok
}

func regularSkillFileState(path string) (string, os.FileInfo, bool) {
	pathInfo, err := os.Lstat(path)
	if err != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() {
		return "", nil, false
	}
	file, err := os.Open(path)
	if err != nil {
		return "", nil, false
	}
	defer func() { _ = file.Close() }()
	openInfo, err := file.Stat()
	if err != nil || !openInfo.Mode().IsRegular() || !os.SameFile(pathInfo, openInfo) {
		return "", nil, false
	}
	body, err := io.ReadAll(file)
	if err != nil {
		return "", nil, false
	}
	currentInfo, err := os.Lstat(path)
	if err != nil || currentInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(openInfo, currentInfo) {
		return "", nil, false
	}
	return rawSkillHash(body), openInfo, true
}

func removeExactSkillFile(path, hash string) error {
	actual, ok := regularSkillFileHash(path)
	if !ok || actual != hash {
		return fmt.Errorf("file is not exactly owned")
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return &filesystemDurabilityError{err: err}
	}
	return nil
}

type skillRemovalIdentity struct {
	dir   os.FileInfo
	final os.FileInfo
}

func removeManagedSkill(root, manifestPath string, manifest *skillManifest, entryIndex int, transition func(string, string)) (bool, error) {
	entry := manifest.Entries[entryIndex]
	dir := filepath.Join(root, entry.Name)
	_, exact, dirInfo := exactSkillDirectoryState(dir, entry.RawSHA256)
	finalHash, finalInfo, finalRegular := regularSkillFileState(filepath.Join(root, entry.Files[0]))
	if !exact || !finalRegular || finalHash != entry.RawSHA256 {
		return resolveRemovalConflict(root, manifestPath, manifest, entryIndex)
	}
	backup := skillRemovalBackupPrefix + newIdempotencyToken()
	if pathExists(filepath.Join(dir, backup)) {
		return false, fmt.Errorf("removal backup already exists")
	}
	entry.PendingRemove = &pendingSkillRemoval{Phase: skillRemovalPlanned, Backup: backup}
	if err := persistSkillEntry(manifestPath, manifest, entryIndex, entry); err != nil {
		return false, err
	}
	return resumePendingRemoval(root, manifestPath, manifest, entryIndex, &skillRemovalIdentity{dir: dirInfo, final: finalInfo}, transition)
}

func resumePendingRemoval(root, manifestPath string, manifest *skillManifest, entryIndex int, identity *skillRemovalIdentity, transition func(string, string)) (bool, error) {
	for {
		entry := manifest.Entries[entryIndex]
		pending := entry.PendingRemove
		dir := filepath.Join(root, entry.Name)
		finalPath := filepath.Join(root, entry.Files[0])
		backupPath := filepath.Join(dir, pending.Backup)
		finalExists, finalExact, finalInfo := exactSkillFile(finalPath, entry.RawSHA256)
		backupExists, backupExact, _ := exactSkillFile(backupPath, entry.RawSHA256)
		if identity == nil && pending.Phase == skillRemovalPlanned {
			if backupExists && !backupExact {
				return false, irregularSkillStateError(backupPath)
			}
			if !backupExact {
				entry.PendingRemove = nil
				if err := persistSkillEntry(manifestPath, manifest, entryIndex, entry); err != nil {
					return false, err
				}
				return true, nil
			}
			if finalExists {
				return resolveRemovalConflict(root, manifestPath, manifest, entryIndex)
			}
			entry = withSkillRemovalPhase(entry, skillRemovalMoved)
			if err := persistSkillEntry(manifestPath, manifest, entryIndex, entry); err != nil {
				return false, err
			}
			continue
		}

		switch pending.Phase {
		case skillRemovalPlanned:
			if !finalExists && backupExact {
				entry = withSkillRemovalPhase(entry, skillRemovalMoved)
				if err := persistSkillEntry(manifestPath, manifest, entryIndex, entry); err != nil {
					return false, err
				}
				continue
			}
			dirInfo, err := os.Lstat(dir)
			if err != nil || dirInfo.Mode()&os.ModeSymlink != 0 || !dirInfo.IsDir() || !finalExact || backupExists || identity != nil && (!os.SameFile(identity.dir, dirInfo) || !os.SameFile(identity.final, finalInfo)) {
				return resolveRemovalConflict(root, manifestPath, manifest, entryIndex)
			}
			if transition != nil {
				transition("remove-before-move", entry.Name)
				dirInfo, err = os.Lstat(dir)
				finalExists, finalExact, finalInfo = exactSkillFile(finalPath, entry.RawSHA256)
				if err != nil || dirInfo.Mode()&os.ModeSymlink != 0 || !dirInfo.IsDir() || !finalExact || pathExists(backupPath) || identity != nil && (!os.SameFile(identity.dir, dirInfo) || !os.SameFile(identity.final, finalInfo)) {
					return resolveRemovalConflict(root, manifestPath, manifest, entryIndex)
				}
			}
			if err := renameNoReplace(finalPath, backupPath); err != nil {
				return false, &manifestPersistenceError{err: err}
			}
			if err := syncSkillStateDirectory(root, dir); err != nil {
				return false, &manifestPersistenceError{err: &filesystemDurabilityError{err: err}}
			}
			backupHash, backupInfo, backupRegular := regularSkillFileState(backupPath)
			if !backupRegular || backupHash != entry.RawSHA256 || !os.SameFile(finalInfo, backupInfo) {
				return resolveRemovalConflict(root, manifestPath, manifest, entryIndex)
			}
			entry = withSkillRemovalPhase(entry, skillRemovalMoved)
			if err := persistSkillEntry(manifestPath, manifest, entryIndex, entry); err != nil {
				return false, err
			}

		case skillRemovalMoved:
			if transition != nil {
				transition("remove-after-move", entry.Name)
				finalExists, _, _ = exactSkillFile(finalPath, entry.RawSHA256)
				backupExists, backupExact, _ = exactSkillFile(backupPath, entry.RawSHA256)
			}
			if backupExists && !backupExact {
				return resolveRemovalConflict(root, manifestPath, manifest, entryIndex)
			}
			if backupExact {
				if err := removeExactSkillFile(backupPath, entry.RawSHA256); err != nil {
					return false, &manifestPersistenceError{err: err}
				}
			}
			if err := syncSkillStateDirectory(root, dir); err != nil {
				return false, &manifestPersistenceError{err: &filesystemDurabilityError{err: err}}
			}
			if finalExists {
				return resolveRemovalConflict(root, manifestPath, manifest, entryIndex)
			}
			if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir() {
				if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTEMPTY) && !errors.Is(err, syscall.EEXIST) {
					return false, &manifestPersistenceError{err: err}
				}
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return false, &manifestPersistenceError{err: err}
			}
			if err := syncDirectory(root); err != nil {
				return false, &manifestPersistenceError{err: &filesystemDurabilityError{err: err}}
			}
			if transition != nil {
				transition("remove-after-directory", entry.Name)
			}
			if err := relinquishManagedSkill(manifestPath, manifest, entryIndex, false); err != nil {
				return false, err
			}
			return true, nil
		}
	}
}

func resolveRemovalConflict(root, manifestPath string, manifest *skillManifest, entryIndex int) (bool, error) {
	entry := manifest.Entries[entryIndex]
	if entry.PendingRemove != nil {
		dir := filepath.Join(root, entry.Name)
		finalPath := filepath.Join(root, entry.Files[0])
		backupPath := filepath.Join(dir, entry.PendingRemove.Backup)
		backupExists, backupExact, _ := exactSkillFile(backupPath, entry.RawSHA256)
		if backupExists && !backupExact {
			return false, irregularSkillStateError(backupPath)
		}
		if !pathExists(finalPath) && backupExact {
			if err := renameNoReplace(backupPath, finalPath); err != nil {
				return false, &manifestPersistenceError{err: err}
			}
			if err := syncDirectory(dir); err != nil {
				return false, &manifestPersistenceError{err: &filesystemDurabilityError{err: err}}
			}
		}
		if backupExact && pathExists(backupPath) {
			if err := removeExactSkillFile(backupPath, entry.RawSHA256); err != nil {
				return false, &manifestPersistenceError{err: err}
			}
		}
		if err := syncSkillStateDirectory(root, dir); err != nil {
			return false, &manifestPersistenceError{err: &filesystemDurabilityError{err: err}}
		}
	}
	if err := relinquishManagedSkill(manifestPath, manifest, entryIndex, true); err != nil {
		return false, err
	}
	return true, nil
}

func relinquishManagedSkill(manifestPath string, manifest *skillManifest, entryIndex int, conflict bool) error {
	before := cloneManifest(*manifest)
	entry := manifest.Entries[entryIndex]
	manifest.Entries = append(manifest.Entries[:entryIndex], manifest.Entries[entryIndex+1:]...)
	if conflict {
		setSkillException(manifest, entry.Deployment, entry.Name, string(components.StatusConflictSkipped), true)
	} else {
		clearSkillException(manifest, entry.Deployment, entry.Name)
	}
	if err := writeSkillManifest(manifestPath, *manifest); err != nil {
		*manifest = before
		return &manifestPersistenceError{err: err}
	}
	return nil
}

func syncSkillStateDirectory(root, dir string) error {
	if pathExists(dir) {
		return syncDirectory(dir)
	}
	return syncDirectory(root)
}

func removeAuthorizedSkills(ctx context.Context, root, manifestPath string, deployment skillDeployment, manifest *skillManifest, transition func(string, string)) (bool, error) {
	changed := false
	names := make([]string, 0)
	for _, entry := range manifest.Entries {
		if entry.Deployment == deployment {
			names = append(names, entry.Name)
		}
	}
	for _, name := range names {
		if ctx.Err() != nil {
			return changed, ctx.Err()
		}
		entryIndex := ownEntryIndex(*manifest, deployment, name)
		if entryIndex < 0 {
			continue
		}
		removed, err := removeManagedSkill(root, manifestPath, manifest, entryIndex, transition)
		if err != nil {
			var persistenceErr *manifestPersistenceError
			if errors.As(err, &persistenceErr) {
				return changed, err
			}
			continue
		}
		changed = changed || removed
	}
	return changed, nil
}

func removePendingInstall(manifest *skillManifest, deployment skillDeployment, name string) {
	for i, install := range manifest.PendingInstalls {
		if install.Deployment == deployment && install.Name == name {
			manifest.PendingInstalls = append(manifest.PendingInstalls[:i], manifest.PendingInstalls[i+1:]...)
			return
		}
	}
}

func skillSyncContext(outcome skillSyncOutcome) string {
	parts := make([]string, 0, 2)
	if len(outcome.InstalledNew) > 0 {
		var b strings.Builder
		b.WriteString("Speakeasy installed new Claude skills for this session. Before using any listed skill, Read its SKILL.md at the absolute path shown and follow it.\n")
		for i, skill := range outcome.InstalledNew {
			if i >= 20 {
				fmt.Fprintf(&b, "- ... and %d more skills\n", len(outcome.InstalledNew)-i)
				break
			}
			line := "- " + skill.Name + " (" + skill.Path + ")"
			if description := truncateUTF8(strings.TrimSpace(skill.Description), 256); description != "" {
				line += ": " + description
			}
			line += "\n"
			if b.Len()+len(line) > skillContextMax {
				b.WriteString("- ... additional skills omitted\n")
				break
			}
			b.WriteString(line)
		}
		parts = append(parts, truncateUTF8(b.String(), skillContextMax))
	}
	if len(outcome.Readonly) > 0 {
		parts = append(parts, readonlySkillContext(outcome.Readonly))
	}
	return strings.Join(parts, "\n\n")
}

func readonlySkillContext(notices []readonlySkillNotice) string {
	var b strings.Builder
	header := "Speakeasy could not write the following distributed skills to disk. Treat the inlined SKILL.md bodies as read-only fallback instructions for this session.\n"
	b.WriteString(header)
	omitted := 0
	for i, skill := range notices {
		description := truncateUTF8(strings.TrimSpace(skill.Description), 256)
		prefix := "\n## " + skill.Name + "\n"
		if description != "" {
			prefix += description + "\n"
		}
		prefix += "<skill-md>\n"
		suffix := "</skill-md>\n"
		remaining := skillReadonlyContextMax - b.Len() - len(prefix) - len(suffix) - skillContextOmittedSpace
		if remaining <= 0 {
			omitted = len(notices) - i
			break
		}
		bodyLimit := min(skillBodyMax, remaining)
		content := truncateUTF8(skill.Content, bodyLimit)
		truncation := ""
		if len(content) < len(skill.Content) {
			truncation = "\n[SKILL.md truncated]\n"
			if len(content)+len(truncation) > remaining {
				content = truncateUTF8(content, max(0, remaining-len(truncation)))
			}
		} else if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		section := prefix + content + truncation + suffix
		if b.Len()+len(section)+skillContextOmittedSpace > skillReadonlyContextMax {
			omitted = len(notices) - i
			break
		}
		b.WriteString(section)
	}
	if omitted > 0 {
		summary := fmt.Sprintf("\n[%d additional skills omitted: 24 KiB fallback limit reached]\n", omitted)
		if b.Len()+len(summary) > skillReadonlyContextMax {
			trimmed := truncateUTF8(b.String(), skillReadonlyContextMax-len(summary))
			b.Reset()
			b.WriteString(trimmed)
		}
		b.WriteString(summary)
	}
	return b.String()
}

func validSkillUpdate(update components.SyncSkillUpdate) bool {
	return validSkillName(update.Name) && validSkillHash(update.RawSha256) && rawSkillHash([]byte(update.Content)) == update.RawSha256
}

func validSkillName(name string) bool {
	if name == "" || len(name) > 64 || !utf8.ValidString(name) {
		return false
	}
	for i, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			continue
		}
		if r != '-' || i == 0 || i == len(name)-1 || name[i-1] == '-' {
			return false
		}
	}
	return true
}

func validSkillHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func rawSkillHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func deploymentKey(deployment skillDeployment) string {
	return deployment.ServerURL + "\x00" + deployment.Org + "\x00" + deployment.Project
}

func ownEntryIndex(manifest skillManifest, deployment skillDeployment, name string) int {
	for i, entry := range manifest.Entries {
		if entry.Deployment == deployment && entry.Name == name {
			return i
		}
	}
	return -1
}

func otherDeploymentOwns(manifest skillManifest, deployment skillDeployment, name string) bool {
	for _, entry := range manifest.Entries {
		if entry.Deployment != deployment && entry.Name == name {
			return true
		}
	}
	return false
}

func permanentSkillConflict(manifest skillManifest, deployment skillDeployment, name string) bool {
	for _, exception := range manifest.Exceptions {
		if exception.Deployment == deployment && exception.Name == name {
			return exception.Permanent
		}
	}
	return false
}

func setSkillException(manifest *skillManifest, deployment skillDeployment, name, status string, permanent bool) bool {
	for i := range manifest.Exceptions {
		exception := &manifest.Exceptions[i]
		if exception.Deployment != deployment || exception.Name != name {
			continue
		}
		if exception.Status == status && exception.Permanent == permanent {
			return false
		}
		exception.Status = status
		exception.Permanent = permanent
		return true
	}
	manifest.Exceptions = append(manifest.Exceptions, managedSkillException{Deployment: deployment, Name: name, Status: status, Permanent: permanent})
	return true
}

func clearSkillException(manifest *skillManifest, deployment skillDeployment, name string) bool {
	for i, exception := range manifest.Exceptions {
		if exception.Deployment == deployment && exception.Name == name {
			manifest.Exceptions = append(manifest.Exceptions[:i], manifest.Exceptions[i+1:]...)
			return true
		}
	}
	return false
}

func cloneManifest(manifest skillManifest) skillManifest {
	entries := make([]managedSkill, len(manifest.Entries))
	for i, entry := range manifest.Entries {
		entries[i] = entry
		entries[i].Files = slices.Clone(entry.Files)
		if entry.PendingUpdate != nil {
			pending := *entry.PendingUpdate
			entries[i].PendingUpdate = &pending
		}
		if entry.PendingRemove != nil {
			pending := *entry.PendingRemove
			entries[i].PendingRemove = &pending
		}
	}
	return skillManifest{
		Version:         manifest.Version,
		Entries:         entries,
		PendingInstalls: slices.Clone(manifest.PendingInstalls),
		Exceptions:      slices.Clone(manifest.Exceptions),
	}
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}
