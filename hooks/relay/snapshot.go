package relay

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/hooks/wire"
)

// clientSnapshot is the client state the binary reports at most once a UTC
// day, on the first hook event it delivers that day. Hooks only run when the
// coding agent fires an event, so the snapshot rides a real event rather
// than a timer. It carries settings and counts only, never prompt or tool
// content.
type clientSnapshot struct {
	day        string
	failOpen   bool
	debugLog   bool
	spoolDepth int
}

type clientSnapshotKey struct{}

var snapshotNow = time.Now

func snapshotMarkerPath() string {
	dir := hooksStateDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "snapshot-day")
}

// withDailySnapshot attaches a snapshot to ctx unless one was already
// delivered today. Concurrent hooks at the start of a day can each send one,
// which is harmless.
func withDailySnapshot(ctx context.Context, cfg Config) context.Context {
	path := snapshotMarkerPath()
	if path == "" {
		return ctx
	}
	today := snapshotNow().UTC().Format(time.DateOnly)
	if b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) == today {
		return ctx
	}
	return context.WithValue(ctx, clientSnapshotKey{}, clientSnapshot{
		day:        today,
		failOpen:   failOpenAllowed(cfg),
		debugLog:   cfg.DebugLog != "",
		spoolDepth: spoolDepth(),
	})
}

// stampSnapshotHeaders adds the snapshot headers when ctx carries one.
func stampSnapshotHeaders(req *http.Request) (clientSnapshot, bool) {
	snap, ok := req.Context().Value(clientSnapshotKey{}).(clientSnapshot)
	if !ok {
		return clientSnapshot{}, false
	}
	req.Header.Set(wire.HeaderDeviceFailOpen, strconv.FormatBool(snap.failOpen))
	req.Header.Set(wire.HeaderDeviceDebugLog, strconv.FormatBool(snap.debugLog))
	req.Header.Set(wire.HeaderDeviceSpoolDepth, strconv.Itoa(snap.spoolDepth))
	return snap, true
}

// markSnapshotDelivered records the day once the server accepted a request
// carrying the snapshot. A failed send leaves the marker alone so the next
// event tries again.
func markSnapshotDelivered(snap clientSnapshot) {
	if path := snapshotMarkerPath(); path != "" {
		_ = atomicWriteCacheFile(path, []byte(snap.day))
	}
}

// spoolDepth counts the entries waiting in the spool to be resent.
func spoolDepth() int {
	dir := spoolDirPath()
	if dir == "" {
		return 0
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, de := range des {
		if _, ok := spoolNanos(de.Name()); ok && !de.IsDir() {
			n++
		}
	}
	return n
}
