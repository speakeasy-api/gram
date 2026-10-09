//go:build linux

package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const credentialsSupported = true

const (
	// credentialBaseName is the agent-owned directory under the root. Only
	// entries inside it are ever created or deleted.
	credentialBaseName = "speakeasy-tunnel-agent"

	// maintenanceLockName serializes instance publication with scavenging.
	maintenanceLockName = ".maintenance.lock"

	// instanceLockName is held by a live agent for its whole lifetime.
	instanceLockName = "lock"

	// maintenanceLockTimeout bounds waiting for another agent's publication
	// or scavenging pass, which only touches directory entries.
	maintenanceLockTimeout = 10 * time.Second

	// maintenanceLockPoll is the retry interval while that lock is held.
	maintenanceLockPoll = 50 * time.Millisecond

	// tempInstanceGrace protects a publishing agent's temporary directory.
	tempInstanceGrace = 5 * time.Minute

	privateDirMode  = 0o700
	privateFileMode = 0o600
)

var (
	instanceNamePattern     = regexp.MustCompile(`^[0-9a-f]{32}$`)
	tempInstanceNamePattern = regexp.MustCompile(`^\.tmp-[0-9a-f]{32}$`)
)

// linuxCredentialStore keeps session files under
// <root>/speakeasy-tunnel-agent/<instance>/. Every object is opened relative
// to an already validated directory descriptor without following symlinks,
// and checked to be ours, private and memory-backed before use.
type linuxCredentialStore struct {
	logger   *slog.Logger
	basePath string
	baseFD   int
	instName string
	instPath string
	instFD   int
	lockFD   int
}

// openCredentialStore validates root, publishes this agent's instance
// directory and scavenges the directories of agents that are gone.
func openCredentialStore(ctx context.Context, root string, logger *slog.Logger) (credentialStore, error) {
	rootFD, err := openTrustedRoot(root)
	if err != nil {
		return nil, fmt.Errorf("credentials root %s: %w", root, err)
	}
	defer func() { _ = unix.Close(rootFD) }()
	if err := requireMemoryBacked(rootFD); err != nil {
		return nil, fmt.Errorf("credentials root %s: %w", root, err)
	}
	if err := requireSameObject(rootFD, root); err != nil {
		return nil, err
	}

	baseFD, err := openOrCreatePrivateDir(rootFD, credentialBaseName)
	if err != nil {
		return nil, fmt.Errorf("credentials directory: %w", err)
	}
	s := &linuxCredentialStore{
		logger:   logger,
		basePath: filepath.Join(root, credentialBaseName),
		baseFD:   baseFD,
		instName: "",
		instPath: "",
		instFD:   -1,
		lockFD:   -1,
	}

	unlock, err := s.lockMaintenance(ctx)
	if err != nil {
		_ = unix.Close(baseFD)
		return nil, err
	}
	defer unlock()
	if err := s.publishInstance(); err != nil {
		_ = unix.Close(baseFD)
		return nil, err
	}
	s.scavenge()
	return s, nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate name: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// requireMemoryBacked refuses filesystems that may write pages to disk on
// their own. tmpfs can still swap; that host property is a documented
// deployment prerequisite.
func requireMemoryBacked(fd int) error {
	var st unix.Statfs_t
	if err := unix.Fstatfs(fd, &st); err != nil {
		return fmt.Errorf("statfs: %w", err)
	}
	switch int64(st.Type) {
	case unix.TMPFS_MAGIC, unix.RAMFS_MAGIC:
		return nil
	default:
		return errors.New("must be on a memory-backed filesystem (tmpfs or ramfs)")
	}
}

// openTrustedRoot opens root one component at a time from "/", without
// following symlinks, and checks that no other user can rename or replace
// anything on the way: server processes reopen the token file by pathname,
// so a path another user could redirect would hand them a different file.
// Every directory must belong to root or the agent's user and be writable
// only by its owner, unless it is sticky (like /dev/shm), where others
// cannot rename entries they do not own.
func openTrustedRoot(root string) (int, error) {
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open /: %w", err)
	}
	if err := requireTrustedDir(fd); err != nil {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("/: %w", err)
	}
	for _, name := range strings.Split(strings.TrimPrefix(root, "/"), "/") {
		if name == "" {
			continue
		}
		next, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(fd)
		if err != nil {
			return -1, fmt.Errorf("open %s: %w", name, err)
		}
		fd = next
		if err := requireTrustedDir(fd); err != nil {
			_ = unix.Close(fd)
			return -1, fmt.Errorf("%s: %w", name, err)
		}
	}
	return fd, nil
}

// requireTrustedDir checks that only root or the agent's user can change
// the directory's entries, short of a sticky directory's own entries.
func requireTrustedDir(fd int) error {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	if st.Uid != 0 && int(st.Uid) != unix.Geteuid() {
		return errors.New("owned by another user")
	}
	if st.Mode&0o022 != 0 && st.Mode&unix.S_ISVTX == 0 {
		return errors.New("writable by other users without the sticky bit")
	}
	return nil
}

// requireSameObject checks that path, which is handed to server processes,
// names the directory we opened.
func requireSameObject(fd int, path string) error {
	var opened, named unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		return fmt.Errorf("stat credentials root: %w", err)
	}
	if err := unix.Stat(path, &named); err != nil {
		return fmt.Errorf("stat credentials root: %w", err)
	}
	if opened.Dev != named.Dev || opened.Ino != named.Ino {
		return errors.New("credentials root changed while it was opened")
	}
	return nil
}

// requirePrivate checks fd is ours and exactly mode perm, of the given type.
func requirePrivate(fd int, fileType uint32, perm uint32) (unix.Stat_t, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return st, fmt.Errorf("stat: %w", err)
	}
	switch {
	case st.Mode&unix.S_IFMT != fileType:
		return st, errors.New("unexpected file type")
	case int(st.Uid) != unix.Geteuid():
		return st, errors.New("not owned by the agent's user")
	case st.Mode&0o7777 != perm:
		return st, fmt.Errorf("permissions are %#o, want %#o", st.Mode&0o7777, perm)
	}
	return st, nil
}

func openPrivateDir(parentFD int, name string) (int, error) {
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open %s: %w", name, err)
	}
	if _, err := requirePrivate(fd, unix.S_IFDIR, privateDirMode); err != nil {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("%s: %w", name, err)
	}
	if err := requireMemoryBacked(fd); err != nil {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("%s: %w", name, err)
	}
	return fd, nil
}

func makePrivateDir(parentFD int, name string) (int, error) {
	if err := unix.Mkdirat(parentFD, name, privateDirMode); err != nil {
		return -1, fmt.Errorf("create %s: %w", name, err)
	}
	// Mkdirat is subject to the umask; set the exact mode.
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open %s: %w", name, err)
	}
	if err := unix.Fchmod(fd, privateDirMode); err != nil {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("chmod %s: %w", name, err)
	}
	_ = unix.Close(fd)
	return openPrivateDir(parentFD, name)
}

func openOrCreatePrivateDir(parentFD int, name string) (int, error) {
	fd, err := openPrivateDir(parentFD, name)
	if err == nil || !errors.Is(err, unix.ENOENT) {
		return fd, err
	}
	fd, err = makePrivateDir(parentFD, name)
	if errors.Is(err, unix.EEXIST) {
		return openPrivateDir(parentFD, name)
	}
	return fd, err
}

// openLockFile opens a lock file, creating it when create is set.
func openLockFile(dirFD int, name string, create bool) (int, error) {
	flags := unix.O_RDWR | unix.O_NOFOLLOW | unix.O_CLOEXEC
	if create {
		flags |= unix.O_CREAT
	}
	fd, err := unix.Openat(dirFD, name, flags, privateFileMode)
	if err != nil {
		return -1, fmt.Errorf("open %s: %w", name, err)
	}
	if create {
		_ = unix.Fchmod(fd, privateFileMode)
	}
	if _, err := requirePrivate(fd, unix.S_IFREG, privateFileMode); err != nil {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("%s: %w", name, err)
	}
	return fd, nil
}

func tryLock(fd int) bool {
	return unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB) == nil
}

// lockMaintenance takes the base directory's maintenance lock. The lock file
// is never removed, so every agent contends on the same inode.
func (s *linuxCredentialStore) lockMaintenance(ctx context.Context) (func(), error) {
	fd, err := openLockFile(s.baseFD, maintenanceLockName, true)
	if err != nil {
		return nil, err
	}
	deadline := time.NewTimer(maintenanceLockTimeout)
	defer deadline.Stop()
	for !tryLock(fd) {
		select {
		case <-ctx.Done():
			_ = unix.Close(fd)
			return nil, ctx.Err()
		case <-deadline.C:
			_ = unix.Close(fd)
			return nil, errors.New("timed out waiting for another tunnel agent's credentials maintenance")
		case <-time.After(maintenanceLockPoll):
		}
	}
	return func() {
		_ = unix.Flock(fd, unix.LOCK_UN)
		_ = unix.Close(fd)
	}, nil
}

// publishInstance creates this agent's directory under a temporary name,
// takes its lock, and only then gives it its final name. The caller holds
// the maintenance lock.
func (s *linuxCredentialStore) publishInstance() error {
	tmpSuffix, err := randomHex(16)
	if err != nil {
		return err
	}
	name, err := randomHex(16)
	if err != nil {
		return err
	}
	tmp := ".tmp-" + tmpSuffix
	fd, err := makePrivateDir(s.baseFD, tmp)
	if err != nil {
		return fmt.Errorf("instance directory: %w", err)
	}
	lockFD, err := unix.Openat(fd, instanceLockName, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, privateFileMode)
	if err == nil && !tryLock(lockFD) {
		_ = unix.Close(lockFD)
		err = errors.New("lock instance directory")
	}
	if err == nil {
		err = unix.Renameat(s.baseFD, tmp, s.baseFD, name)
		if err != nil {
			_ = unix.Close(lockFD)
		}
	}
	if err != nil {
		_ = removeAllAt(fd)
		_ = unix.Close(fd)
		_ = unix.Unlinkat(s.baseFD, tmp, unix.AT_REMOVEDIR)
		return fmt.Errorf("publish instance directory: %w", err)
	}
	s.instName, s.instPath, s.instFD, s.lockFD = name, filepath.Join(s.basePath, name), fd, lockFD
	return nil
}

// scavenge removes directories of agents that are no longer running: those
// whose instance lock is free. The caller holds the maintenance lock, so a
// temporary directory it sees belongs to an agent that died while
// publishing. Names this agent never creates are left alone.
func (s *linuxCredentialStore) scavenge() {
	names, err := readDirNames(s.baseFD)
	if err != nil {
		s.logger.Warn("tunnel credentials scavenge could not list instances", slog.Any("error", err))
		return
	}
	for _, name := range names {
		if name == s.instName {
			continue
		}
		temp := tempInstanceNamePattern.MatchString(name)
		if !temp && !instanceNamePattern.MatchString(name) {
			continue
		}
		if err := s.scavengeOne(name, temp); err != nil {
			s.logger.Warn("tunnel credentials scavenge skipped an instance directory", slog.Any("error", err))
		}
	}
}

func (s *linuxCredentialStore) scavengeOne(name string, temp bool) error {
	fd, err := openPrivateDir(s.baseFD, name)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(fd) }()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return fmt.Errorf("stat %s: %w", name, err)
	}
	if temp && time.Since(time.Unix(st.Mtim.Unix())) < tempInstanceGrace {
		return nil
	}
	sameInstance := func() bool {
		var now unix.Stat_t
		return unix.Fstatat(s.baseFD, name, &now, unix.AT_SYMLINK_NOFOLLOW) == nil && now.Dev == st.Dev && now.Ino == st.Ino
	}
	lockFD, err := openLockFile(fd, instanceLockName, temp)
	if errors.Is(err, unix.ENOENT) {
		// Cleanup removes the lock last, so a finalized instance without
		// one was interrupted just before its own removal. Remove it only
		// if it is empty; anything else is left alone.
		if sameInstance() && unix.Unlinkat(s.baseFD, name, unix.AT_REMOVEDIR) == nil {
			return nil
		}
		return fmt.Errorf("instance %s has no lock and is not empty", name)
	}
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(lockFD) }()
	if !tryLock(lockFD) {
		return nil
	}
	if !sameInstance() {
		return fmt.Errorf("instance %s changed while scavenging", name)
	}
	if err := removeInstance(s.baseFD, name, fd); err != nil {
		return fmt.Errorf("remove stale instance: %w", err)
	}
	s.logger.Info("tunnel credentials removed a stale agent's files")
	return nil
}

// removeInstance deletes an instance directory whose lock the caller holds.
// The lock goes last: if anything else cannot be removed, the lock stays so
// a later scavenger can recognize the instance and finish the job.
func removeInstance(baseFD int, name string, instFD int) error {
	if err := removeAllExcept(instFD, instanceLockName); err != nil {
		return fmt.Errorf("remove contents: %w", err)
	}
	if err := unix.Unlinkat(instFD, instanceLockName, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("remove lock: %w", err)
	}
	if err := unix.Unlinkat(baseFD, name, unix.AT_REMOVEDIR); err != nil {
		return fmt.Errorf("remove directory: %w", err)
	}
	return nil
}

func readDirNames(dirFD int) ([]string, error) {
	// A fresh descriptor so reading entries never moves dirFD's offset.
	fd, err := unix.Openat(dirFD, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open directory: %w", err)
	}
	defer func() { _ = unix.Close(fd) }()
	var names []string
	buf := make([]byte, 8<<10)
	for {
		n, err := unix.Getdents(fd, buf)
		if err != nil {
			return nil, fmt.Errorf("read directory: %w", err)
		}
		if n <= 0 {
			return names, nil
		}
		_, _, entries := unix.ParseDirent(buf[:n], -1, nil)
		names = append(names, entries...)
	}
}

// removeAllAt deletes everything inside dirFD without following symlinks or
// crossing into another filesystem.
func removeAllAt(dirFD int) error {
	return removeAllExcept(dirFD, "")
}

// removeAllExcept is removeAllAt that keeps the top-level entry keep.
func removeAllExcept(dirFD int, keep string) error {
	var parent unix.Stat_t
	if err := unix.Fstat(dirFD, &parent); err != nil {
		return fmt.Errorf("stat directory: %w", err)
	}
	names, err := readDirNames(dirFD)
	if err != nil {
		return err
	}
	var errs []error
	for _, name := range names {
		if keep != "" && name == keep {
			continue
		}
		var st unix.Stat_t
		if err := unix.Fstatat(dirFD, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			if !errors.Is(err, unix.ENOENT) {
				errs = append(errs, err)
			}
			continue
		}
		if st.Mode&unix.S_IFMT != unix.S_IFDIR {
			if err := unix.Unlinkat(dirFD, name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
				errs = append(errs, err)
			}
			continue
		}
		if st.Dev != parent.Dev {
			errs = append(errs, fmt.Errorf("%s is another filesystem", name))
			continue
		}
		child, err := unix.Openat(dirFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := removeAllAt(child); err != nil {
			errs = append(errs, err)
		}
		_ = unix.Close(child)
		if err := unix.Unlinkat(dirFD, name, unix.AT_REMOVEDIR); err != nil && !errors.Is(err, unix.ENOENT) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *linuxCredentialStore) createSession() (credentialDir, error) {
	suffix, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	name := "s-" + suffix
	fd, err := makePrivateDir(s.instFD, name)
	if err != nil {
		return nil, fmt.Errorf("session directory: %w", err)
	}
	d := &linuxCredentialDir{store: s, name: name, fd: fd, path: filepath.Join(s.instPath, name), removeOnce: sync.Once{}, removeErr: nil}
	homeFD, err := makePrivateDir(fd, "home")
	if err != nil {
		_ = d.remove()
		return nil, fmt.Errorf("session home: %w", err)
	}
	_ = unix.Close(homeFD)
	return d, nil
}

// Close removes the instance while holding its lock. Final removal of the
// lock and directory happens under the maintenance lock, like scavenging; if
// that lock cannot be had, or anything else cannot be removed, the lock file
// stays for a later agent's scavenger.
func (s *linuxCredentialStore) Close() error {
	defer func() {
		_ = unix.Close(s.instFD)
		_ = unix.Close(s.lockFD)
		_ = unix.Close(s.baseFD)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), maintenanceLockTimeout)
	defer cancel()
	unlock, err := s.lockMaintenance(ctx)
	if err != nil {
		return errors.Join(err, removeAllExcept(s.instFD, instanceLockName))
	}
	defer unlock()
	return removeInstance(s.baseFD, s.instName, s.instFD)
}

type linuxCredentialDir struct {
	store *linuxCredentialStore
	name  string
	fd    int
	path  string

	// removeOnce makes remove idempotent: it closes fd.
	removeOnce sync.Once
	removeErr  error
}

const tokenFileName = "token"

func (d *linuxCredentialDir) tokenPath() string { return filepath.Join(d.path, tokenFileName) }

func (d *linuxCredentialDir) homePath() string { return filepath.Join(d.path, "home") }

func (d *linuxCredentialDir) writeToken(token string) error {
	suffix, err := randomHex(8)
	if err != nil {
		return err
	}
	tmp := ".token-" + suffix
	fd, err := unix.Openat(d.fd, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, privateFileMode)
	if err != nil {
		return fmt.Errorf("create token file: %w", err)
	}
	err = func() error {
		defer func() { _ = unix.Close(fd) }()
		if err := unix.Fchmod(fd, privateFileMode); err != nil {
			return fmt.Errorf("chmod token file: %w", err)
		}
		if err := requireMemoryBacked(fd); err != nil {
			return fmt.Errorf("token file: %w", err)
		}
		for buf := []byte(token); len(buf) > 0; {
			n, err := unix.Write(fd, buf)
			if err != nil {
				if errors.Is(err, syscall.EINTR) {
					continue
				}
				return fmt.Errorf("write token file: %w", err)
			}
			buf = buf[n:]
		}
		return nil
	}()
	if err == nil {
		err = unix.Renameat(d.fd, tmp, d.fd, tokenFileName)
	}
	if err != nil {
		_ = unix.Unlinkat(d.fd, tmp, 0)
		return err
	}
	return nil
}

func (d *linuxCredentialDir) remove() error {
	d.removeOnce.Do(func() {
		err := removeAllAt(d.fd)
		_ = unix.Close(d.fd)
		if uerr := unix.Unlinkat(d.store.instFD, d.name, unix.AT_REMOVEDIR); uerr != nil && !errors.Is(uerr, unix.ENOENT) {
			err = errors.Join(err, uerr)
		}
		d.removeErr = err
	})
	return d.removeErr
}
