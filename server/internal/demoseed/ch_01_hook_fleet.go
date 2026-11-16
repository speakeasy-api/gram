package demoseed

import (
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/plugins"
)

// chHookFleetInstall is one demo machine as the hooks endpoints record it,
// for the staff hooks fleet view (hook_install_summaries).
type chHookFleetInstall struct {
	handle     string
	host       string
	source     string
	harness    string
	version    string
	route      string
	generation string
	// lastDay is how many days ago the machine last sent hooks. A machine
	// quiet for 3 days or more shows as quiet.
	lastDay int
}

const chHookFleetDays = 7

func chHookFleetInstalls() []chHookFleetInstall {
	latest := plugins.LatestHooksVersion()
	return []chHookFleetInstall{
		{"amara", "", "claude-code", "claude", latest, "marketplace", "ingest_binary", 0},
		{"jonas", "", "claude-code", "claude", latest, "marketplace", "ingest_binary", 0},
		{"priya", "", "codex", "codex", latest, "download", "ingest_binary", 0},
		{"priya", "priya-linux-dev", "cursor", "cursor", "0.3.20", "download", "ingest_binary", 1},
		{"mateo", "", "claude-code", "claude", "0.2.9", "download", "ingest_binary", 5},
		{"hana", "", "cowork", "", "", "", "legacy_keyed", 0},
		{"lucas", "", "claude-code", "claude", latest, "marketplace", "ingest_script", 0},
	}
}

// chHookFleetRows are SessionStart hook rows carrying the attributes the
// hooks endpoints stamp, one per machine per day.
func (s *seeder) chHookFleetRows() []chTelemetryRow {
	byHandle := map[string]demoUser{}
	for _, u := range demoUsers {
		byHandle[u.handle] = u
	}
	var rows []chTelemetryRow
	for i, install := range chHookFleetInstalls() {
		u := byHandle[install.handle]
		host := install.host
		if host == "" {
			host = u.hostname
		}
		device := ""
		if install.harness != "" {
			device += `,"gram.hook.device.harness":"` + install.harness + `"`
		}
		if install.version != "" {
			device += `,"gram.hook.device.binary_version":"` + install.version + `"`
		}
		if install.route != "" {
			device += `,"gram.hook.device.install_route":"` + install.route + `"`
		}
		for d := install.lastDay; d < install.lastDay+chHookFleetDays; d++ {
			ts := s.now.Add(-time.Duration(d)*24*time.Hour - time.Duration(hash64("hookfleet", i, d)%3600)*time.Second).UnixNano()
			rows = append(rows, chTelemetryRow{
				timeNano:     ts,
				observedNano: ts,
				severity:     "INFO",
				body:         "Hook: SessionStart",
				traceID:      s.chTrace(fmt.Sprintf("hookfleettrace-%d-%d", i, d)),
				attributes: `{"gram.event.source":"hook"` +
					`,"gram.hook.source":"` + install.source + `"` +
					`,"gram.hook.event":"SessionStart"` +
					`,"gram.hook.endpoint_generation":"` + install.generation + `"` +
					device +
					`,"gram.org.id":"` + s.orgID() + `"` +
					`,"gram.project.id":"` + s.projectID().String() + `"` +
					`,"user.email":"` + s.email(install.handle) + `"` +
					`,"gram.hook.hostname":"` + host + `"}`,
				resource: s.chResource(),
				urn:      "",
				service:  "",
				chatID:   nil,
			})
		}
	}
	return rows
}
