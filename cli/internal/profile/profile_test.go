package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/keys"
)

// These tests point HOME and XDG_CONFIG_HOME at temp dirs, so they cannot run
// in parallel.

func setHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	return home
}

func writeConfig(t *testing.T, path string, config *Config) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	data, err := json.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

func readConfig(t *testing.T, path string) *Config {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)
	var config Config
	require.NoError(t, json.Unmarshal(data, &config))
	return &config
}

func legacyConfig() *Config {
	return &Config{
		Current: "default",
		Profiles: map[string]*Profile{
			"default": {Secret: "legacy-secret", DefaultProjectSlug: "proj", APIUrl: "https://app.getgram.ai", Org: nil, Projects: []*keys.ValidateKeyProject{{ID: "1", Name: "Proj", Slug: "proj"}, {ID: "2", Name: "Other", Slug: "other"}}},
			"staging": {Secret: "staging-secret", DefaultProjectSlug: "", APIUrl: "https://staging.example.com", Org: nil, Projects: nil},
		},
	}
}

func TestDefaultProfilePath_UnderConfigDir(t *testing.T) { //nolint:paralleltest // sets HOME and XDG_CONFIG_HOME
	home := setHome(t)

	path, err := DefaultProfilePath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".config", "speakeasy-ai", "profile.json"), path)

	legacy, err := LegacyProfilePath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".gram", "profile.json"), legacy)
}

func TestDefaultProfilePath_RespectsXDGConfigHome(t *testing.T) {
	setHome(t)
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	path, err := DefaultProfilePath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(xdg, "speakeasy-ai", "profile.json"), path)

	// A relative XDG_CONFIG_HOME is invalid and ignored.
	t.Setenv("XDG_CONFIG_HOME", "relative/dir")
	path, err = DefaultProfilePath()
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(path))
	require.NotContains(t, path, "relative")
}

func TestLoad_FallsBackToLegacyFile(t *testing.T) { //nolint:paralleltest // sets HOME and XDG_CONFIG_HOME
	home := setHome(t)
	writeConfig(t, filepath.Join(home, ".gram", "profile.json"), legacyConfig())
	path, err := DefaultProfilePath()
	require.NoError(t, err)

	prof, err := Load("")
	require.NoError(t, err)
	require.Equal(t, "legacy-secret", prof.Secret)

	prof, err = LoadByName(path, "staging")
	require.NoError(t, err)
	require.Equal(t, "staging-secret", prof.Secret)

	// Reading never writes the new file.
	require.NoFileExists(t, path)
}

func TestLoad_NewFileWinsOverLegacy(t *testing.T) { //nolint:paralleltest // sets HOME and XDG_CONFIG_HOME
	home := setHome(t)
	writeConfig(t, filepath.Join(home, ".gram", "profile.json"), legacyConfig())
	path, err := DefaultProfilePath()
	require.NoError(t, err)
	writeConfig(t, path, &Config{Current: "default", Profiles: map[string]*Profile{"default": {Secret: "new-secret", DefaultProjectSlug: "", APIUrl: "", Org: nil, Projects: nil}}})

	prof, err := LoadByName(path, "")
	require.NoError(t, err)
	require.Equal(t, "new-secret", prof.Secret)

	prof, err = LoadByName(path, "staging")
	require.NoError(t, err)
	require.Nil(t, prof, "profiles only in the legacy file are not merged in")
}

func TestLoad_ExplicitPathDoesNotFallBack(t *testing.T) { //nolint:paralleltest // sets HOME and XDG_CONFIG_HOME
	home := setHome(t)
	writeConfig(t, filepath.Join(home, ".gram", "profile.json"), legacyConfig())

	prof, err := LoadByName(filepath.Join(t.TempDir(), "profile.json"), "")
	require.NoError(t, err)
	require.Nil(t, prof)
}

func TestLoad_NoFiles(t *testing.T) { //nolint:paralleltest // sets HOME and XDG_CONFIG_HOME
	setHome(t)

	prof, err := Load("")
	require.NoError(t, err)
	require.Nil(t, prof)
}

func TestSave_MigratesLegacyProfilesOnFirstWrite(t *testing.T) { //nolint:paralleltest // sets HOME and XDG_CONFIG_HOME
	home := setHome(t)
	legacyPath := filepath.Join(home, ".gram", "profile.json")
	writeConfig(t, legacyPath, legacyConfig())
	legacyBefore, err := os.ReadFile(filepath.Clean(legacyPath))
	require.NoError(t, err)
	path, err := DefaultProfilePath()
	require.NoError(t, err)

	require.NoError(t, UpdateProjectSlug(path, "other"))

	migrated := readConfig(t, path)
	require.Equal(t, "default", migrated.Current)
	require.Equal(t, "other", migrated.Profiles["default"].DefaultProjectSlug)
	require.Equal(t, "legacy-secret", migrated.Profiles["default"].Secret)
	require.Equal(t, "staging-secret", migrated.Profiles["staging"].Secret)

	info, err := os.Stat(path)
	require.NoError(t, err)
	if os.PathSeparator == '/' {
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	// The legacy file is left untouched for older CLI releases.
	legacyAfter, err := os.ReadFile(filepath.Clean(legacyPath))
	require.NoError(t, err)
	require.Equal(t, legacyBefore, legacyAfter)
}

func TestUpdateOrCreate_KeepsOtherLegacyProfiles(t *testing.T) { //nolint:paralleltest // sets HOME and XDG_CONFIG_HOME
	home := setHome(t)
	writeConfig(t, filepath.Join(home, ".gram", "profile.json"), legacyConfig())
	path, err := DefaultProfilePath()
	require.NoError(t, err)

	require.NoError(t, UpdateOrCreate("new-key", "https://app.getgram.ai", nil, nil, path, "default", ""))

	saved := readConfig(t, path)
	require.Equal(t, "new-key", saved.Profiles["default"].Secret)
	require.Equal(t, "staging-secret", saved.Profiles["staging"].Secret)
}

func TestClear_AlsoEmptiesLegacyFile(t *testing.T) { //nolint:paralleltest // sets HOME and XDG_CONFIG_HOME
	home := setHome(t)
	legacyPath := filepath.Join(home, ".gram", "profile.json")
	writeConfig(t, legacyPath, legacyConfig())
	path, err := DefaultProfilePath()
	require.NoError(t, err)

	require.NoError(t, Clear(path))

	require.Empty(t, readConfig(t, path).Profiles)
	require.Empty(t, readConfig(t, legacyPath).Profiles)
	prof, err := LoadByName(path, "")
	require.NoError(t, err)
	require.Nil(t, prof)
}

func TestClear_WithoutLegacyFileCreatesNone(t *testing.T) { //nolint:paralleltest // sets HOME and XDG_CONFIG_HOME
	home := setHome(t)
	path, err := DefaultProfilePath()
	require.NoError(t, err)

	require.NoError(t, Clear(path))

	require.FileExists(t, path)
	require.NoFileExists(t, filepath.Join(home, ".gram", "profile.json"))
}

func TestSave_WritesThroughSymlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "profile.json")
	writeConfig(t, target, EmptyConfig())
	link := filepath.Join(dir, "profile.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	require.NoError(t, Save(legacyConfig(), link))

	info, err := os.Lstat(link)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "the link was replaced")
	require.Equal(t, "legacy-secret", readConfig(t, target).Profiles["default"].Secret)
}

func TestSave_WritesThroughDanglingSymlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "dotfiles"), 0o700))
	link := filepath.Join(dir, "profile.json")
	if err := os.Symlink(filepath.Join("dotfiles", "profile.json"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	require.NoError(t, Save(legacyConfig(), link))

	info, err := os.Lstat(link)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "the link was replaced")
	require.Equal(t, "legacy-secret", readConfig(t, filepath.Join(dir, "dotfiles", "profile.json")).Profiles["default"].Secret)
}

func TestClear_ReportsUnreadableLegacyFile(t *testing.T) { //nolint:paralleltest // sets HOME and XDG_CONFIG_HOME
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	home := setHome(t)
	legacyDir := filepath.Join(home, ".gram")
	writeConfig(t, filepath.Join(legacyDir, "profile.json"), legacyConfig())
	require.NoError(t, os.Chmod(legacyDir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(legacyDir, 0o700) }) // #nosec G302 -- restores a test directory.

	path, err := DefaultProfilePath()
	require.NoError(t, err)
	require.ErrorContains(t, Clear(path), "check legacy profile file")
}
