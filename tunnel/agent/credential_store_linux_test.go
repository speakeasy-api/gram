//go:build linux

package agent

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/speakeasy-api/gram/tunnel/identity"
)

// memoryRoot returns a fresh directory on a memory-backed filesystem. CI
// runs these tests on Linux, where /dev/shm is tmpfs; a host without one
// fails rather than silently skipping the storage guarantees.
func memoryRoot(t *testing.T) string {
	t.Helper()
	var st unix.Statfs_t
	require.NoError(t, unix.Statfs("/dev/shm", &st), "credentials tests need /dev/shm")
	require.Contains(t, []int64{unix.TMPFS_MAGIC, unix.RAMFS_MAGIC}, int64(st.Type), "credentials tests need /dev/shm on tmpfs")
	root, err := os.MkdirTemp("/dev/shm", "tunnel-agent-test-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func openTestStore(t *testing.T, root string) *linuxCredentialStore {
	t.Helper()
	store, err := openCredentialStore(t.Context(), root, discardLogger())
	require.NoError(t, err)
	return store.(*linuxCredentialStore)
}

func instanceNames(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, credentialBaseName))
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		if e.Name() != maintenanceLockName {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestCredentialStoreWritesPrivateTokenFiles(t *testing.T) {
	t.Parallel()
	root := memoryRoot(t)
	store := openTestStore(t, root)

	dir, err := store.createSession()
	require.NoError(t, err)
	require.NoError(t, dir.writeToken(testTokenA))
	require.NoError(t, dir.writeToken(testTokenB))

	content, err := os.ReadFile(dir.tokenPath())
	require.NoError(t, err)
	require.Equal(t, identity.TokenSHA256(testTokenB), identity.TokenSHA256(string(content)))
	for path, mode := range map[string]os.FileMode{
		filepath.Join(root, credentialBaseName): 0o700 | os.ModeDir,
		filepath.Dir(dir.tokenPath()):           0o700 | os.ModeDir,
		dir.homePath():                          0o700 | os.ModeDir,
		dir.tokenPath():                         0o600,
	} {
		info, err := os.Lstat(path)
		require.NoError(t, err)
		require.Equal(t, mode, info.Mode(), path)
	}
	entries, err := os.ReadDir(filepath.Dir(dir.tokenPath()))
	require.NoError(t, err)
	require.Len(t, entries, 2, "no temporary token files are left behind")

	require.NoError(t, dir.remove())
	_, err = os.Stat(filepath.Dir(dir.tokenPath()))
	require.ErrorIs(t, err, os.ErrNotExist)

	require.NoError(t, store.Close())
	require.Empty(t, instanceNames(t, root))
}

func TestCredentialStoreRefusesUnsafeRoots(t *testing.T) {
	t.Parallel()
	_, err := openCredentialStore(t.Context(), diskDir(t), discardLogger())
	require.ErrorContains(t, err, "memory-backed")

	root := memoryRoot(t)
	target := memoryRoot(t)
	require.NoError(t, os.Symlink(target, filepath.Join(root, "link")))
	_, err = openCredentialStore(t.Context(), filepath.Join(root, "link"), discardLogger())
	require.ErrorContains(t, err, "use the resolved path", "a symlinked root is refused with a clear reason")

	require.NoError(t, os.Symlink(target, filepath.Join(root, credentialBaseName)))
	_, err = openCredentialStore(t.Context(), root, discardLogger())
	require.Error(t, err, "a symlinked agent directory is refused")

	open := memoryRoot(t)
	require.NoError(t, os.Mkdir(filepath.Join(open, credentialBaseName), 0o755))
	require.NoError(t, os.Chmod(filepath.Join(open, credentialBaseName), 0o755))
	_, err = openCredentialStore(t.Context(), open, discardLogger())
	require.ErrorContains(t, err, "permissions")
}

// diskDir returns a fresh directory on a filesystem that is not memory
// backed, failing when the host has none.
func diskDir(t *testing.T) string {
	t.Helper()
	for _, parent := range []string{os.TempDir(), "/var/tmp", "/root"} {
		dir, err := os.MkdirTemp(parent, "tunnel-agent-disk-")
		if err != nil {
			continue
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		var st unix.Statfs_t
		if unix.Statfs(dir, &st) == nil && st.Type != unix.TMPFS_MAGIC && st.Type != unix.RAMFS_MAGIC {
			return dir
		}
	}
	t.Fatal("credentials tests need a disk-backed directory to prove it is refused")
	return ""
}

func TestCredentialTokenRefusedOnDiskBackedDirectory(t *testing.T) {
	t.Parallel()
	disk := diskDir(t)
	fd, err := unix.Open(disk, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	require.NoError(t, err)
	d := &linuxCredentialDir{store: nil, name: "x", fd: fd, path: disk}
	t.Cleanup(func() { _ = unix.Close(fd) })
	require.ErrorContains(t, d.writeToken(testTokenA), "memory-backed")
	entries, err := os.ReadDir(disk)
	require.NoError(t, err)
	require.Empty(t, entries, "nothing is left on disk")
}

func TestCredentialStoresShareARootSafely(t *testing.T) {
	t.Parallel()
	root := memoryRoot(t)
	first := openTestStore(t, root)
	firstDir, err := first.createSession()
	require.NoError(t, err)
	require.NoError(t, firstDir.writeToken(testTokenA))

	second := openTestStore(t, root)
	_, err = os.Stat(firstDir.tokenPath())
	require.NoError(t, err, "starting a second agent leaves a live agent's files alone")

	require.NoError(t, os.WriteFile(filepath.Join(root, "unrelated"), []byte("x"), 0o600))
	require.NoError(t, second.Close())
	_, err = os.Stat(firstDir.tokenPath())
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "unrelated"))
	require.NoError(t, err)
	require.NoError(t, first.Close())
}

func TestCredentialStoreScavengesCrashedAgent(t *testing.T) {
	t.Parallel()
	root := memoryRoot(t)
	crashed := openTestStore(t, root)
	dir, err := crashed.createSession()
	require.NoError(t, err)
	require.NoError(t, dir.writeToken(testTokenA))
	// A crash releases the lock without removing anything.
	simulateCrash(t, crashed, dir)

	survivor := openTestStore(t, root)
	_, err = os.Stat(dir.tokenPath())
	require.ErrorIs(t, err, os.ErrNotExist, "a dead agent's token is removed at the next start")
	require.Equal(t, []string{survivor.instName}, instanceNames(t, root))
	require.NoError(t, survivor.Close())
}

func TestCredentialStoreScavengingRules(t *testing.T) {
	t.Parallel()
	root := memoryRoot(t)
	store := openTestStore(t, root)
	base := filepath.Join(root, credentialBaseName)
	old := time.Now().Add(-2 * tempInstanceGrace)

	young := filepath.Join(base, ".tmp-"+"0123456789abcdef0123456789abcdef")
	require.NoError(t, os.Mkdir(young, 0o700))
	aged := filepath.Join(base, ".tmp-"+"fedcba9876543210fedcba9876543210")
	require.NoError(t, os.Mkdir(aged, 0o700))
	require.NoError(t, os.Chtimes(aged, old, old))
	unknown := filepath.Join(base, "not-an-instance")
	require.NoError(t, os.Mkdir(unknown, 0o700))
	// Interrupted between removing its lock and its directory.
	emptyLockless := filepath.Join(base, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.NoError(t, os.Mkdir(emptyLockless, 0o700))
	lockless := filepath.Join(base, "dddddddddddddddddddddddddddddddd")
	require.NoError(t, os.Mkdir(lockless, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(lockless, "unknown"), nil, 0o600))
	// A stale instance whose lock was replaced with a symlink.
	swapped := filepath.Join(base, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	require.NoError(t, os.Mkdir(swapped, 0o700))
	outside := filepath.Join(memoryRoot(t), "victim")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(swapped, instanceLockName)))
	// A stale instance holding a symlink to a file outside it.
	stale := filepath.Join(base, "cccccccccccccccccccccccccccccccc")
	require.NoError(t, os.Mkdir(stale, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(stale, instanceLockName), nil, 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(stale, "escape")))

	store.scavenge()

	for path, wantExists := range map[string]bool{young: true, aged: false, unknown: true, emptyLockless: false, lockless: true, swapped: true, stale: false, outside: true} {
		_, err := os.Lstat(path)
		if wantExists {
			require.NoError(t, err, path)
		} else {
			require.ErrorIs(t, err, os.ErrNotExist, path)
		}
	}
	require.NoError(t, store.Close())
}

func TestCredentialStorePublicationIsSerializedWithScavenging(t *testing.T) {
	t.Parallel()
	root := memoryRoot(t)
	holder := openTestStore(t, root)
	base := filepath.Join(root, credentialBaseName)

	// A publisher paused between creating its temporary directory and
	// locking it, past the grace period.
	unlock, err := holder.lockMaintenance(t.Context())
	require.NoError(t, err)
	paused := ".tmp-" + "11111111111111111111111111111111"
	require.NoError(t, os.Mkdir(filepath.Join(base, paused), 0o700))
	old := time.Now().Add(-2 * tempInstanceGrace)
	require.NoError(t, os.Chtimes(filepath.Join(base, paused), old, old))

	started := make(chan *linuxCredentialStore, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { started <- openTestStore(t, root) })
	}
	select {
	case <-started:
		t.Fatal("scavenging must wait for an in-progress publication")
	case <-time.After(300 * time.Millisecond):
	}

	// The paused publisher finishes and goes live.
	fd, err := unix.Open(filepath.Join(base, paused), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	require.NoError(t, err)
	lockFD, err := unix.Openat(fd, instanceLockName, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC, 0o600)
	require.NoError(t, err)
	require.NoError(t, unix.Flock(lockFD, unix.LOCK_EX|unix.LOCK_NB))
	live := "22222222222222222222222222222222"
	require.NoError(t, os.Rename(filepath.Join(base, paused), filepath.Join(base, live)))
	require.NoError(t, os.WriteFile(filepath.Join(base, live, "s-keep"), nil, 0o600))
	unlock()

	wg.Wait()
	close(started)
	for store := range started {
		t.Cleanup(func() { _ = store.Close() })
	}
	_, err = os.Stat(filepath.Join(base, live, "s-keep"))
	require.NoError(t, err, "a publisher that went live is never scavenged")
	require.NoError(t, unix.Close(lockFD))
	require.NoError(t, unix.Close(fd))
	require.NoError(t, holder.Close())
}

func TestCredentialsModeEndToEndOnLinux(t *testing.T) {
	t.Parallel()
	root := memoryRoot(t)
	signer := newTestSigner(t)
	exe, err := os.Executable()
	require.NoError(t, err)
	cfg := signer.config()
	cfg.Root = root
	// Escapes the process group, as a daemonizing child would.
	escaped := filepath.Join(t.TempDir(), "escaped.pid")
	a, err := New(Config{
		GatewayURL:       "wss://example.test/connect",
		APIKey:           "gram_tunnel_test",
		LocalMCPURL:      "",
		LocalMCPCommand:  "setsid sh -c 'echo $$ > " + escaped + "; exec sleep 300' & exec env " + stdioFixtureEnv + "=1 " + stdioFixtureReadTokenAtStart + "=1 '" + exe + "'",
		StdioMaxSessions: 0,
		StdioIdleTimeout: 0,
		StdioCredentials: &cfg,
		ServiceVersion:   "1.0.0",
		Metadata:         map[string]string{},
		MinBackoff:       0,
		MaxBackoff:       0,
	}, discardLogger())
	require.NoError(t, err)
	srv := httptest.NewServer(a.handler)
	t.Cleanup(srv.Close)
	c := &credentialTestServer{srv: srv, bridge: a.stdio, signer: signer, store: nil, clock: newTestClock(), logs: &syncBuffer{}}

	call := credentialCall{token: testTokenA}
	call.sid = c.initialize(t, call)
	require.Equal(t, identity.TokenSHA256(testTokenA), c.toolText(t, call, "token-sha"))
	tokenPath := a.stdio.session(call.sid).cred.dir.tokenPath()
	require.Equal(t, root, tokenPath[:len(root)])

	require.Equal(t, http.StatusNoContent, c.do(t, credentialCall{sid: call.sid, method: http.MethodDelete}).StatusCode)
	require.Eventually(t, func() bool {
		_, err := os.Stat(tokenPath)
		return os.IsNotExist(err)
	}, 30*time.Second, 50*time.Millisecond, "an escaped descendant loses the token file")

	a.stdio.Close()
	entries, err := os.ReadDir(filepath.Join(root, credentialBaseName))
	require.NoError(t, err)
	require.Len(t, entries, 1, "only the maintenance lock remains after shutdown")

	if pid, err := os.ReadFile(escaped); err == nil {
		var n int
		if _, err := fmt.Sscan(string(pid), &n); err == nil && n > 0 {
			_ = unix.Kill(n, unix.SIGKILL)
		}
	}
}

func TestCredentialTokenFollowsTheOpenedDirectory(t *testing.T) {
	t.Parallel()
	root := memoryRoot(t)
	store := openTestStore(t, root)
	dir, err := store.createSession()
	require.NoError(t, err)
	sessionPath := filepath.Dir(dir.tokenPath())

	// Swap the session directory for a symlink to somewhere else.
	moved := sessionPath + "-moved"
	require.NoError(t, os.Rename(sessionPath, moved))
	elsewhere := memoryRoot(t)
	require.NoError(t, os.Symlink(elsewhere, sessionPath))

	require.NoError(t, dir.writeToken(testTokenA))
	entries, err := os.ReadDir(elsewhere)
	require.NoError(t, err)
	require.Empty(t, entries, "the token never follows a swapped-in symlink")
	_, err = os.Stat(filepath.Join(moved, tokenFileName))
	require.NoError(t, err, "it lands in the directory that was opened")

	require.NoError(t, os.Remove(sessionPath))
	require.NoError(t, os.Rename(moved, sessionPath))
	require.NoError(t, dir.remove())
	require.NoError(t, store.Close())
}

func TestCredentialTokenWriteFailsWhenDirectoryIsGone(t *testing.T) {
	t.Parallel()
	root := memoryRoot(t)
	store := openTestStore(t, root)
	dir, err := store.createSession()
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(filepath.Dir(dir.tokenPath())))
	require.Error(t, dir.writeToken(testTokenA))
	_ = dir.remove()
	require.NoError(t, store.Close())
}

func TestCredentialRootMustBeSafeFromOtherUsers(t *testing.T) {
	t.Parallel()
	open := memoryRoot(t)
	require.NoError(t, os.Chmod(open, 0o777))
	_, err := openCredentialStore(t.Context(), open, discardLogger())
	require.ErrorContains(t, err, "without the sticky bit", "anyone could rename the agent's directory")

	sticky := memoryRoot(t)
	require.NoError(t, os.Chmod(sticky, 0o777|os.ModeSticky))
	store := openTestStore(t, sticky)
	require.NoError(t, store.Close())

	parent := memoryRoot(t)
	require.NoError(t, os.Chmod(parent, 0o777))
	nested := filepath.Join(parent, "nested")
	require.NoError(t, os.Mkdir(nested, 0o700))
	_, err = openCredentialStore(t.Context(), nested, discardLogger())
	require.Error(t, err, "an unsafe ancestor is refused too")
}

// requireOtherUserEnv makes the other-user tests fail instead of skipping;
// CI sets it when it reruns them as root.
const requireOtherUserEnv = "TUNNEL_TEST_REQUIRE_OTHER_USER"

// otherUser runs a command as an unprivileged user other than the agent's.
// It needs root to switch users, so it runs where the tests run as root, such
// as the Linux container used for local verification; CI runs as a regular
// user and covers the same refusal through TestCredentialRootMustBeSafeFromOtherUsers.
func otherUser(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	required := os.Getenv(requireOtherUserEnv) == "1"
	if os.Geteuid() != 0 {
		if required {
			t.Fatal("switching to another user needs root")
		}
		t.Skip("switching to another user needs root")
	}
	setpriv, err := exec.LookPath("setpriv")
	if err != nil {
		if required {
			t.Fatal("setpriv is not installed")
		}
		t.Skip("setpriv is not installed")
	}
	return exec.Command(setpriv, append([]string{"--reuid=2000", "--regid=2000", "--clear-groups"}, args...)...)
}

func TestCredentialTokenPathCannotBeRedirectedByAnotherUser(t *testing.T) {
	t.Parallel()
	root := memoryRoot(t)
	require.NoError(t, os.Chmod(root, 0o777|os.ModeSticky))
	store := openTestStore(t, root)
	dir, err := store.createSession()
	require.NoError(t, err)

	base := filepath.Join(root, credentialBaseName)
	out, err := otherUser(t, "mv", base, filepath.Join(root, "moved")).CombinedOutput()
	require.Error(t, err, "another user cannot move the agent's directory out of a sticky root: %s", out)
	out, err = otherUser(t, "mkdir", "-p", filepath.Join(root, "decoy")).CombinedOutput()
	require.NoError(t, err, string(out))

	require.NoError(t, dir.writeToken(testTokenA))
	read, err := os.ReadFile(dir.tokenPath())
	require.NoError(t, err)
	require.Equal(t, identity.TokenSHA256(testTokenA), identity.TokenSHA256(string(read)), "the server's pathname reaches the token the agent wrote")
	require.NoError(t, dir.remove())
	require.NoError(t, store.Close())

	foreign := memoryRoot(t)
	require.NoError(t, os.Chown(foreign, 2000, 2000))
	_, err = openCredentialStore(t.Context(), foreign, discardLogger())
	require.ErrorContains(t, err, "owned by another user")
}

func TestCredentialCleanupFailureKeepsRecoveryLock(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permissions this test uses to make removal fail")
	}
	root := memoryRoot(t)
	store := openTestStore(t, root)
	dir, err := store.createSession()
	require.NoError(t, err)
	require.NoError(t, dir.writeToken(testTokenA))
	sessionPath := filepath.Dir(dir.tokenPath())
	instance := store.instPath

	require.NoError(t, os.Chmod(sessionPath, 0o000))
	require.Error(t, store.Close(), "the session directory cannot be removed")
	_, err = os.Stat(filepath.Join(instance, instanceLockName))
	require.NoError(t, err, "the lock stays so the instance can be recovered")

	require.NoError(t, os.Chmod(sessionPath, 0o700))
	next := openTestStore(t, root)
	_, err = os.Stat(dir.tokenPath())
	require.ErrorIs(t, err, os.ErrNotExist, "the next agent removes the token once the fault is repaired")
	_, err = os.Stat(instance)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, next.Close())
}

// Not parallel: the umask is process-wide, and top-level parallel tests only
// start once the sequential ones have finished.
//
//nolint:paralleltest // Changes the process umask.
func TestCredentialStoreRecoversCrashUnderRestrictiveUmask(t *testing.T) {
	root := memoryRoot(t)
	previous := unix.Umask(0o277)
	crashed, err := openCredentialStore(t.Context(), root, discardLogger())
	unix.Umask(previous)
	require.NoError(t, err)
	store := crashed.(*linuxCredentialStore)

	info, err := os.Stat(filepath.Join(store.instPath, instanceLockName))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the lock is usable by a later scavenger")

	dir, err := store.createSession()
	require.NoError(t, err)
	require.NoError(t, dir.writeToken(testTokenA))
	simulateCrash(t, store, dir)

	survivor := openTestStore(t, root)
	_, err = os.Stat(dir.tokenPath())
	require.ErrorIs(t, err, os.ErrNotExist, "a crashed instance is recovered at the next start")
	require.NoError(t, survivor.Close())
}

// simulateCrash closes a store's descriptors without removing anything, as
// an agent that dies does. The store must not be used afterwards.
func simulateCrash(t *testing.T, store *linuxCredentialStore, dirs ...credentialDir) {
	t.Helper()
	for _, dir := range dirs {
		require.NoError(t, unix.Close(dir.(*linuxCredentialDir).fd))
	}
	for _, fd := range []int{store.lockFD, store.instFD, store.baseFD} {
		require.NoError(t, unix.Close(fd))
	}
}
