package relay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// orgSettings is the persisted mirror of the org-level settings the server
// returns in ingest response effects. It is the offline copy consulted when a
// gating event cannot obtain a verdict because the control plane is
// unreachable or failing; every successful ingest refreshes it, so it always
// holds the last server-confirmed value.
type orgSettings struct {
	// ServerURL scopes the cache to the deployment it was learned from — a
	// value cached from a dev server must not govern production enforcement.
	ServerURL string `json:"server_url"`
	// Org scopes the cache within a server, mirroring readCachedAuth: another
	// org's posture must not apply here.
	Org string `json:"org,omitempty"`
	// FailOpen is the org's downtime choice: allow gating events through when
	// no verdict is obtainable instead of blocking until recovery.
	FailOpen  bool      `json:"fail_open"`
	UpdatedAt time.Time `json:"updated_at"`
}

// orgSettingsMaxAge bounds how long a cached posture may govern enforcement
// without server confirmation: a machine offline past it (a drawer laptop
// with a long-stale fail-open) reverts to the fail-closed default rather than
// honoring a choice the org may have reversed long ago.
const orgSettingsMaxAge = 14 * 24 * time.Hour

// orgSettingsRefreshAge is how old an unchanged cache entry may grow before a
// successful exchange rewrites it anyway. Unchanged values normally skip the
// write, so without this periodic refresh a continuously-syncing machine's
// updated_at would never advance and the entry would age out spuriously.
const orgSettingsRefreshAge = 24 * time.Hour

// orgSettingsPath returns the settings cache location, a sibling of the
// credential cache so it follows the GRAM_HOOKS_AUTH_FILE override. It is
// deliberately not removed by forgetAuth: losing a credential must not flip
// the org's enforcement posture.
func orgSettingsPath() string {
	return authFilePath() + ".org-settings.json"
}

// readOrgSettings loads the cached settings, enforcing the same server/org
// scoping rules as readCachedAuth plus the max-age cutoff. A missing,
// malformed, mismatched, or stale cache reads as absent — the caller falls
// back to the fail-closed default.
func readOrgSettings(cfg Config) (orgSettings, bool) {
	b, err := os.ReadFile(orgSettingsPath())
	if err != nil {
		return orgSettings{}, false
	}
	var s orgSettings
	if err := json.Unmarshal(b, &s); err != nil {
		return orgSettings{}, false
	}
	if s.ServerURL != cfg.ServerURL {
		return orgSettings{}, false
	}
	if cfg.OrgID != "" && s.Org != "" && s.Org != cfg.OrgID {
		return orgSettings{}, false
	}
	if time.Since(s.UpdatedAt) > orgSettingsMaxAge {
		return orgSettings{}, false
	}
	return s, true
}

// writeOrgSettings persists a server-confirmed fail-open value. Recently
// confirmed unchanged values are skipped so the common case does no I/O
// beyond the read; an aged entry is rewritten even when unchanged so its
// updated_at keeps clearing the max-age cutoff. Failures are swallowed: a
// hook must never fail over its settings cache, and a stale or absent cache
// only means the fail-closed default applies.
func writeOrgSettings(cfg Config, org string, failOpen bool) {
	if org == "" {
		org = cfg.OrgID
	}
	if cur, ok := readOrgSettings(cfg); ok && cur.FailOpen == failOpen && cur.Org == org && time.Since(cur.UpdatedAt) < orgSettingsRefreshAge {
		return
	}

	path := orgSettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	body, err := json.Marshal(orgSettings{
		ServerURL: cfg.ServerURL,
		Org:       org,
		FailOpen:  failOpen,
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		return
	}
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

// failOpenAllowed reports whether an unobtainable verdict (server unreachable
// or 5xx) may fail open: the GRAM_HOOKS_FAIL_OPEN escape hatch, or the org's
// last server-confirmed setting. Absent both, gating events fail closed.
func failOpenAllowed(cfg Config) bool {
	if v := strings.TrimSpace(os.Getenv("GRAM_HOOKS_FAIL_OPEN")); v == "1" || strings.EqualFold(v, "true") {
		return true
	}
	s, ok := readOrgSettings(cfg)
	return ok && s.FailOpen
}
