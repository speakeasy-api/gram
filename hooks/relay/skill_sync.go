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
	skillRemovalPrefix       = ".gram-remove-"
	skillContextOmittedSpace = 80
)

type skillDeployment struct {
	ServerURL string `json:"server_url"`
	Org       string `json:"org"`
	Project   string `json:"project"`
}

type pendingSkillUpdate struct {
	RawSHA256 string `json:"raw_sha256"`
}

type pendingSkillRemoval struct {
	Tombstone string `json:"tombstone"`
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

type skillRemovalTombstone struct {
	Deployment skillDeployment `json:"deployment"`
	Name       string          `json:"name"`
	Files      []string        `json:"files"`
	RawSHA256  string          `json:"raw_sha256"`
	Tombstone  string          `json:"tombstone"`
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
	Tombstones      []skillRemovalTombstone `json:"removal_tombstones,omitempty"`
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
		_, _ = removeAuthorizedSkills(ctx, root, manifestPath, deployment, &manifest)
		return ""
	}
	if result == nil {
		return ""
	}

	outcome := applySkillSyncResult(ctx, root, manifestPath, deployment, &manifest, result)
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
		Tombstones:      []skillRemovalTombstone{},
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
		if entry.PendingUpdate != nil && !validSkillHash(entry.PendingUpdate.RawSHA256) {
			return false
		}
		if entry.PendingRemove != nil && !validStateName(entry.PendingRemove.Tombstone, skillRemovalPrefix) {
			return false
		}
		if entry.PendingRemove != nil {
			if _, exists := stateNames[entry.PendingRemove.Tombstone]; exists {
				return false
			}
			stateNames[entry.PendingRemove.Tombstone] = struct{}{}
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
	for _, tombstone := range manifest.Tombstones {
		if !validOwnedSkill(tombstone.Name, tombstone.Files, tombstone.RawSHA256) || !validStateName(tombstone.Tombstone, skillRemovalPrefix) {
			return false
		}
		if _, exists := stateNames[tombstone.Tombstone]; exists {
			return false
		}
		stateNames[tombstone.Tombstone] = struct{}{}
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
	sort.Slice(manifest.Tombstones, func(i, j int) bool { return manifest.Tombstones[i].Tombstone < manifest.Tombstones[j].Tombstone })
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
			if err := renameDirNoReplace(tempPath, finalPath); err != nil {
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

	for i := len(manifest.Entries) - 1; i >= 0; i-- {
		entry := manifest.Entries[i]
		if entry.PendingUpdate != nil {
			diskHash, ok := managedSkillHash(root, entry)
			switch {
			case ok && diskHash == entry.PendingUpdate.RawSHA256:
				entry.RawSHA256 = entry.PendingUpdate.RawSHA256
				entry.PendingUpdate = nil
				manifest.Entries[i] = entry
			case ok && diskHash == entry.RawSHA256:
				entry.PendingUpdate = nil
				manifest.Entries[i] = entry
			default:
				manifest.Entries = append(manifest.Entries[:i], manifest.Entries[i+1:]...)
				setSkillException(manifest, entry.Deployment, entry.Name, string(components.StatusConflictSkipped), true)
			}
			changed = true
			continue
		}
		if entry.PendingRemove == nil {
			continue
		}
		finalPath := filepath.Join(root, entry.Name)
		tombPath := filepath.Join(root, entry.PendingRemove.Tombstone)
		finalExists, finalExact := exactSkillDirectory(finalPath, entry.RawSHA256)
		tombExists, tombExact := exactSkillDirectory(tombPath, entry.RawSHA256)
		switch {
		case finalExact && !tombExists:
			if err := os.Rename(finalPath, tombPath); err != nil {
				return changed, err
			}
			if err := syncDirectory(root); err != nil {
				return changed, &manifestPersistenceError{err: &filesystemDurabilityError{err: err}}
			}
			manifest.Tombstones = append(manifest.Tombstones, tombstoneFromEntry(entry))
		case !finalExists && tombExact:
			manifest.Tombstones = append(manifest.Tombstones, tombstoneFromEntry(entry))
		case !finalExists && !tombExists:
		case finalExists:
			if tombExact {
				manifest.Tombstones = append(manifest.Tombstones, tombstoneFromEntry(entry))
			}
			setSkillException(manifest, entry.Deployment, entry.Name, string(components.StatusConflictSkipped), true)
		default:
			// An irregular tombstone is preserved and no longer owned.
		}
		manifest.Entries = append(manifest.Entries[:i], manifest.Entries[i+1:]...)
		changed = true
	}
	if changed {
		if err := writeSkillManifest(manifestPath, *manifest); err != nil {
			return changed, &manifestPersistenceError{err: err}
		}
	}

	cleanupBefore := cloneManifest(*manifest)
	cleanupChanged := false
	for i := len(manifest.Tombstones) - 1; i >= 0; i-- {
		tombstone := manifest.Tombstones[i]
		path := filepath.Join(root, tombstone.Tombstone)
		exists, exact := exactSkillDirectory(path, tombstone.RawSHA256)
		empty := emptyRegularDirectory(path)
		if exists && !exact && !empty {
			continue
		}
		if exact {
			if err := removeExactSkillDirectory(path, tombstone.RawSHA256); err != nil {
				var durabilityErr *filesystemDurabilityError
				if errors.As(err, &durabilityErr) {
					return changed || cleanupChanged, &manifestPersistenceError{err: err}
				}
				continue
			}
		} else if empty {
			if err := os.Remove(path); err != nil {
				continue
			}
			if err := syncDirectory(root); err != nil {
				return changed || cleanupChanged, &manifestPersistenceError{err: &filesystemDurabilityError{err: err}}
			}
		}
		manifest.Tombstones = append(manifest.Tombstones[:i], manifest.Tombstones[i+1:]...)
		cleanupChanged = true
	}
	if cleanupChanged {
		if err := writeSkillManifest(manifestPath, *manifest); err != nil {
			*manifest = cleanupBefore
			return true, &manifestPersistenceError{err: err}
		}
	}
	return changed || cleanupChanged, nil
}

func reconcileOwnedSkills(root string, deployment skillDeployment, manifest *skillManifest) bool {
	changed := false
	kept := manifest.Entries[:0]
	for _, entry := range manifest.Entries {
		if entry.Deployment != deployment {
			kept = append(kept, entry)
			continue
		}
		if _, intact := intactManagedSkill(root, entry); intact {
			kept = append(kept, entry)
			continue
		}
		setSkillException(manifest, deployment, entry.Name, string(components.StatusConflictSkipped), true)
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
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return true, false
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 1 || entries[0].Name() != "SKILL.md" || entries[0].Type()&os.ModeSymlink != 0 {
		return true, false
	}
	skillInfo, err := os.Lstat(filepath.Join(path, "SKILL.md"))
	if err != nil || !skillInfo.Mode().IsRegular() {
		return true, false
	}
	body, err := os.ReadFile(filepath.Join(path, "SKILL.md"))
	return true, err == nil && rawSkillHash(body) == hash
}

func emptyRegularDirectory(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false
	}
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
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

func applySkillSyncResult(ctx context.Context, root, manifestPath string, deployment skillDeployment, manifest *skillManifest, result *components.SyncSkillsResult) skillSyncOutcome {
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
		removed, err := removeManagedSkill(root, manifestPath, manifest, entryIndex)
		if err != nil {
			var persistenceErr *manifestPersistenceError
			if errors.As(err, &persistenceErr) {
				outcome.PersistenceError = true
				return outcome
			}
			continue
		}
		outcome.DurableChanged = outcome.DurableChanged || removed
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
			updated, err := updateManagedSkill(root, manifestPath, manifest, entryIndex, update)
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
	if err := renameDirNoReplace(tempPath, finalPath); err != nil {
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

func updateManagedSkill(root, manifestPath string, manifest *skillManifest, entryIndex int, update components.SyncSkillUpdate) (bool, error) {
	entry := manifest.Entries[entryIndex]
	entry.PendingUpdate = &pendingSkillUpdate{RawSHA256: update.RawSha256}
	manifest.Entries[entryIndex] = entry
	if err := writeSkillManifest(manifestPath, *manifest); err != nil {
		entry.PendingUpdate = nil
		manifest.Entries[entryIndex] = entry
		return false, &manifestPersistenceError{err: err}
	}
	dirInfo, err := os.Lstat(filepath.Join(root, entry.Name))
	if err != nil || dirInfo.Mode()&os.ModeSymlink != 0 || !dirInfo.IsDir() {
		return clearPendingUpdate(manifestPath, manifest, entryIndex, &skillConflictError{err: fmt.Errorf("managed skill directory changed")})
	}
	diskHash, intact := managedSkillHash(root, entry)
	if !intact || diskHash != entry.RawSHA256 {
		return clearPendingUpdate(manifestPath, manifest, entryIndex, &skillConflictError{err: fmt.Errorf("managed skill content changed")})
	}
	if err := atomicWriteFile(filepath.Join(root, entry.Files[0]), []byte(update.Content), 0o644); err != nil {
		var durabilityErr *filesystemDurabilityError
		if errors.As(err, &durabilityErr) {
			return false, &manifestPersistenceError{err: err}
		}
		if !managedSkillPathRegular(root, entry) {
			return clearPendingUpdate(manifestPath, manifest, entryIndex, &skillConflictError{err: err})
		}
		return clearPendingUpdate(manifestPath, manifest, entryIndex, err)
	}
	pendingState := cloneManifest(*manifest)
	entry.RawSHA256 = update.RawSha256
	entry.PendingUpdate = nil
	manifest.Entries[entryIndex] = entry
	clearSkillException(manifest, entry.Deployment, entry.Name)
	if err := writeSkillManifest(manifestPath, *manifest); err != nil {
		*manifest = pendingState
		return false, &manifestPersistenceError{err: err}
	}
	return true, nil
}

func clearPendingUpdate(manifestPath string, manifest *skillManifest, entryIndex int, cause error) (bool, error) {
	entry := manifest.Entries[entryIndex]
	entry.PendingUpdate = nil
	manifest.Entries[entryIndex] = entry
	if err := writeSkillManifest(manifestPath, *manifest); err != nil {
		return false, &manifestPersistenceError{err: err}
	}
	return false, cause
}

func removeManagedSkill(root, manifestPath string, manifest *skillManifest, entryIndex int) (bool, error) {
	entry := manifest.Entries[entryIndex]
	finalPath := filepath.Join(root, entry.Name)
	_, exact := exactSkillDirectory(finalPath, entry.RawSHA256)
	if !exact {
		return false, fmt.Errorf("managed skill directory contains unowned content")
	}
	tombstoneName := skillRemovalPrefix + newIdempotencyToken()
	tombstonePath := filepath.Join(root, tombstoneName)
	if _, err := os.Lstat(tombstonePath); !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("removal tombstone already exists")
	}
	entry.PendingRemove = &pendingSkillRemoval{Tombstone: tombstoneName}
	manifest.Entries[entryIndex] = entry
	if err := writeSkillManifest(manifestPath, *manifest); err != nil {
		entry.PendingRemove = nil
		manifest.Entries[entryIndex] = entry
		return false, &manifestPersistenceError{err: err}
	}
	if err := os.Rename(finalPath, tombstonePath); err != nil {
		entry.PendingRemove = nil
		manifest.Entries[entryIndex] = entry
		if writeErr := writeSkillManifest(manifestPath, *manifest); writeErr != nil {
			return false, &manifestPersistenceError{err: writeErr}
		}
		return false, err
	}
	if err := syncDirectory(root); err != nil {
		return false, &manifestPersistenceError{err: &filesystemDurabilityError{err: err}}
	}
	pendingState := cloneManifest(*manifest)
	manifest.Entries = append(manifest.Entries[:entryIndex], manifest.Entries[entryIndex+1:]...)
	clearSkillException(manifest, entry.Deployment, entry.Name)
	manifest.Tombstones = append(manifest.Tombstones, tombstoneFromEntry(entry))
	if err := writeSkillManifest(manifestPath, *manifest); err != nil {
		*manifest = pendingState
		return false, &manifestPersistenceError{err: err}
	}
	if err := removeExactSkillDirectory(tombstonePath, entry.RawSHA256); err == nil {
		cleanupState := cloneManifest(*manifest)
		removeTombstone(manifest, tombstoneName)
		if err := writeSkillManifest(manifestPath, *manifest); err != nil {
			*manifest = cleanupState
		}
	} else {
		var durabilityErr *filesystemDurabilityError
		if errors.As(err, &durabilityErr) {
			return false, &manifestPersistenceError{err: err}
		}
	}
	return true, nil
}

func removeAuthorizedSkills(ctx context.Context, root, manifestPath string, deployment skillDeployment, manifest *skillManifest) (bool, error) {
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
		removed, err := removeManagedSkill(root, manifestPath, manifest, entryIndex)
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

func tombstoneFromEntry(entry managedSkill) skillRemovalTombstone {
	return skillRemovalTombstone{Deployment: entry.Deployment, Name: entry.Name, Files: slices.Clone(entry.Files), RawSHA256: entry.RawSHA256, Tombstone: entry.PendingRemove.Tombstone}
}

func removePendingInstall(manifest *skillManifest, deployment skillDeployment, name string) {
	for i, install := range manifest.PendingInstalls {
		if install.Deployment == deployment && install.Name == name {
			manifest.PendingInstalls = append(manifest.PendingInstalls[:i], manifest.PendingInstalls[i+1:]...)
			return
		}
	}
}

func removeTombstone(manifest *skillManifest, name string) {
	for i, tombstone := range manifest.Tombstones {
		if tombstone.Tombstone == name {
			manifest.Tombstones = append(manifest.Tombstones[:i], manifest.Tombstones[i+1:]...)
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
		Tombstones:      slices.Clone(manifest.Tombstones),
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

func managedSkillPathRegular(root string, entry managedSkill) bool {
	dirInfo, err := os.Lstat(filepath.Join(root, entry.Name))
	if err != nil || dirInfo.Mode()&os.ModeSymlink != 0 || !dirInfo.IsDir() {
		return false
	}
	fileInfo, err := os.Lstat(filepath.Join(root, entry.Files[0]))
	return err == nil && fileInfo.Mode()&os.ModeSymlink == 0 && fileInfo.Mode().IsRegular()
}
