// One-command install for a headless device enrolled with a join token
// (a "bootstrap token" in the dashboard).
//
// A copyable command must not carry the join token: a one-liner lands in shell
// history, chat threads and screen shares. So the token is held behind a
// short-lived, single-use install code, and the command carries the code.
// Fetching the script spends the code and returns a script with the token
// inside the managed config it writes, so a leaked command is worthless once
// it has been run.

package workloadpolicy

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/about"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

const (
	// deviceInstallScriptRoute serves the install script for an install code.
	// Unauthenticated: the code is the credential.
	deviceInstallScriptRoute = "/v1/install/device/{code}"

	// deviceInstallScriptPathPrefix is deviceInstallScriptRoute without its
	// code, for building the command.
	deviceInstallScriptPathPrefix = "/v1/install/device/"

	// deviceInstallCodeMaxTTL bounds an install code's life. The code is meant
	// for the person who just asked for the command, on the box in front of
	// them, so it does not need to outlive a short setup session.
	deviceInstallCodeMaxTTL = 15 * time.Minute

	// deviceInstallCodeBytes sizes the install code. It bears a join token for
	// its lifetime, so it carries as much entropy as one: 256 bits.
	deviceInstallCodeBytes = 32

	// deviceAgentDefaultControlPlaneURL is the control plane the device agent
	// talks to when its managed config names none. The config names one only
	// when the request arrived elsewhere.
	deviceAgentDefaultControlPlaneURL = "https://app.getgram.ai"

	// deviceAgentInstallerURL is the release installer for speakeasyd on Linux.
	deviceAgentInstallerURL = about.DeviceAgentReleasesBaseURL + "/install.sh"

	// deviceInstallScriptFile is the file name the download command saves the
	// script under.
	deviceInstallScriptFile = "speakeasy-device-install.sh"
)

// deviceInstallRecord is what an install code stands for. It holds a join
// token, so it lives only in the cache, only until the earlier of the code's
// and the token's expiry, and is deleted on first read.
type deviceInstallRecord struct {
	Code             string `json:"code"`
	JoinToken        string `json:"join_token"`
	ControlPlaneURL  string `json:"control_plane_url"`
	HooksProjectSlug string `json:"hooks_project_slug"`

	// ttl is how long the record is kept. It is decided when the record is
	// created and never read back from the cache.
	ttl time.Duration
}

var _ cache.CacheableObject[deviceInstallRecord] = (*deviceInstallRecord)(nil)

func (r deviceInstallRecord) CacheKey() string   { return deviceInstallCacheKey(r.Code) }
func (r deviceInstallRecord) TTL() time.Duration { return r.ttl }

func deviceInstallCacheKey(code string) string { return "deviceInstall:" + code }

// deviceInstallCodeExpiry is when an install code issued at now stops working:
// deviceInstallCodeMaxTTL later, or when its join token expires if sooner, since
// a script carrying a dead token would only fail on the box.
func deviceInstallCodeExpiry(now, tokenExpiresAt time.Time) time.Time {
	if limit := now.Add(deviceInstallCodeMaxTTL); limit.Before(tokenExpiresAt) {
		return limit
	}
	return tokenExpiresAt
}

func newDeviceInstallCode() (string, error) {
	raw := make([]byte, deviceInstallCodeBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate install code: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Service) CreateDeviceInstallCommand(ctx context.Context, payload *gen.CreateDeviceInstallCommandPayload) (*gen.WorkloadDeviceInstallCommand, error) {
	issuer, err := s.authorizeDeviceJoinToken(ctx, payload.AgentID, payload.ProjectSlugInput)
	if err != nil {
		return nil, err
	}

	if err := s.requireDeviceInstallFlag(ctx, issuer.tenancy.organizationID); err != nil {
		return nil, err
	}

	if _, err := projectsrepo.New(s.db).GetProjectBySlug(ctx, projectsrepo.GetProjectBySlugParams{
		Slug:           payload.HooksProjectSlug,
		OrganizationID: issuer.tenancy.organizationID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeInvalid, err, "hooks_project_slug does not name a project in this organization")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving hooks project").LogError(ctx, s.logger)
	}

	code, err := newDeviceInstallCode()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error generating install code").LogError(ctx, s.logger)
	}
	baseURL := strings.TrimRight(requestorigin.BaseURL(ctx, s.sharedHosts.ServerURL), "/")

	var codeExpiresAt time.Time
	// The record is stored inside the token's transaction, so a cache failure
	// leaves no token behind, and a token is never committed without a way to
	// reach it.
	issued, err := s.issueDeviceJoinToken(ctx, issuer, payload.ExpiresInSeconds, func(issued issuedDeviceJoinToken) error {
		now := time.Now()
		codeExpiresAt = deviceInstallCodeExpiry(now, issued.expiresAt)
		record := deviceInstallRecord{
			Code:             code,
			JoinToken:        issued.token,
			ControlPlaneURL:  baseURL,
			HooksProjectSlug: payload.HooksProjectSlug,
			ttl:              codeExpiresAt.Sub(now),
		}
		if err := s.deviceInstallCache.Store(ctx, record); err != nil {
			return oops.E(oops.CodeUnexpected, err, "error storing install command").LogError(ctx, s.logger)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	scriptURL := shellSingleQuote(baseURL + deviceInstallScriptPathPrefix + code)
	return &gen.WorkloadDeviceInstallCommand{
		Command:         "curl -fsSL " + scriptURL + " | sudo sh",
		DownloadCommand: "curl -fsSL " + scriptURL + " -o " + deviceInstallScriptFile + "\nsudo sh " + deviceInstallScriptFile,
		CodeExpiresAt:   codeExpiresAt.UTC().Format(time.RFC3339),
		JoinTokenID:     issued.id.String(),
		TokenExpiresAt:  issued.expiresAt.UTC().Format(time.RFC3339),
	}, nil
}

// requireDeviceInstallFlag holds the install command closed until the
// organization is in the rollout. An unavailable evaluator counts as closed.
func (s *Service) requireDeviceInstallFlag(ctx context.Context, organizationID string) error {
	orgSlug := ""
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil {
		orgSlug = authCtx.OrganizationSlug
	}
	evaluation, err := feature.EvaluateFlag(ctx, s.features, feature.FlagDeviceBootstrapInstall, organizationID, feature.OrgProjectGroups(orgSlug, ""))
	if err != nil {
		s.logger.WarnContext(ctx, "evaluate device-bootstrap-install flag", attr.SlogError(err))
	}
	if evaluation != feature.EvaluationEnabled {
		return oops.E(oops.CodeNotFound, nil, "device install commands are not available for this organization")
	}
	return nil
}

// handleDeviceInstallScript spends an install code and returns the script it
// stands for. An unknown, expired or spent code are the same to the caller:
// 404 with nothing else. Neither the record nor the code is ever logged.
func (s *Service) handleDeviceInstallScript(mux goahttp.Muxer) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		code := mux.Vars(r)["code"]
		if code == "" {
			return oops.C(oops.CodeNotFound)
		}

		// GetAndDelete, not Get: a code that has been read is spent, whether or
		// not the script reaches the box.
		record, err := s.deviceInstallCache.GetAndDelete(r.Context(), deviceInstallCacheKey(code))
		if err != nil || record.JoinToken == "" {
			return oops.C(oops.CodeNotFound)
		}

		script, err := renderDeviceInstallScript(record)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "error rendering install script").LogError(r.Context(), s.logger)
		}

		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		// The body is a credential. Nothing may keep a copy.
		w.Header().Set("Cache-Control", "no-store")
		if _, err := w.Write([]byte(script)); err != nil {
			return oops.E(oops.CodeUnexpected, err, "error writing install script").LogError(r.Context(), s.logger)
		}
		return nil
	}
}

// shellSingleQuote renders a value as one POSIX single-quoted word, so nothing
// interpolated into a command or script can end the quoting and start a
// command of its own.
func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// deviceManagedConfig is the device agent's managed.json for a device_key
// workload. join_token sits beside workload_identity, not inside it.
type deviceManagedConfig struct {
	// V is the managed config schema version.
	V int `json:"v"`

	// WorkloadIdentity makes the device a workload that proves itself with a
	// key it generates and registers.
	WorkloadIdentity deviceManagedWorkloadIdentity `json:"workload_identity"`

	// JoinToken registers the device key once; the agent ignores it after.
	JoinToken string `json:"join_token"`

	// ControlPlaneURL overrides the agent's default control plane. Omitted when
	// the default is right.
	ControlPlaneURL string `json:"_control_plane_url,omitempty"`
}

type deviceManagedWorkloadIdentity struct {
	// Source is how the workload proves its identity.
	Source string `json:"source"`

	// HooksProjectSlug is the project the device's hooks and telemetry go to.
	HooksProjectSlug string `json:"hooks_project_slug"`
}

// deviceManagedConfigJSON renders the managed config as one line of JSON. The
// script embeds it in a quoted here-document, where the shell expands nothing.
func deviceManagedConfigJSON(record deviceInstallRecord) (string, error) {
	controlPlaneURL := record.ControlPlaneURL
	if controlPlaneURL == deviceAgentDefaultControlPlaneURL {
		controlPlaneURL = ""
	}
	encoded, err := json.Marshal(deviceManagedConfig{
		V: 1,
		WorkloadIdentity: deviceManagedWorkloadIdentity{
			Source:           "device_key",
			HooksProjectSlug: record.HooksProjectSlug,
		},
		JoinToken:       record.JoinToken,
		ControlPlaneURL: controlPlaneURL,
	})
	if err != nil {
		return "", fmt.Errorf("encode managed config: %w", err)
	}
	return string(encoded), nil
}

// deviceManagedConfigDelimiter ends the here-document that carries the managed
// config. JSON encoding keeps the config on one line starting with "{", so the
// delimiter can never appear inside it.
const deviceManagedConfigDelimiter = "SPEAKEASY_MANAGED_CONFIG"

// renderDeviceInstallScript writes the install script for one record.
func renderDeviceInstallScript(record deviceInstallRecord) (string, error) {
	managed, err := deviceManagedConfigJSON(record)
	if err != nil {
		return "", err
	}
	return strings.NewReplacer(
		"@@INSTALLER_URL@@", shellSingleQuote(deviceAgentInstallerURL),
		"@@CONTROL_PLANE_URL@@", shellSingleQuote(record.ControlPlaneURL),
		"@@DELIMITER@@", deviceManagedConfigDelimiter,
		"@@MANAGED_CONFIG@@", managed,
	).Replace(deviceInstallScriptTemplate), nil
}

// deviceInstallScriptTemplate is the install script. Placeholders are
// replaced in one pass, so a value can never introduce another placeholder.
const deviceInstallScriptTemplate = `#!/bin/sh
# Speakeasy device install
#
# Installs the Speakeasy device agent (speakeasyd) on this Linux machine and
# enrolls the machine as a workload with a single-use bootstrap token.
#
# Generated for one use. The managed config below carries a live bootstrap
# token: treat this file as a secret, and delete it once it has run.
#
# Usage:
#   sudo sh speakeasy-device-install.sh [--user NAME] [--force]
#   curl -fsSL <url> | sudo sh -s -- [--user NAME] [--force]
#
#   --user NAME  The account that runs this machine's AI tools. Defaults to
#                the account that ran sudo.
#   --force      Replace an existing /etc/speakeasy/managed.json, keeping a
#                backup next to it.
#
# What it does:
#   1. Checks this is Linux and that it runs as root.
#   2. Picks the account the agent runs as. The agent must run as the account
#      that runs the AI tools: they get their MCP tokens from it, and it
#      writes their configuration. It never runs as root.
#   3. Installs speakeasyd to /usr/local/bin with the release installer.
#   4. Writes /etc/speakeasy/managed.json, owned by root and readable by the
#      account's primary group only (0640). The bootstrap token goes into that
#      file and nowhere else: never onto a command line, never to the screen.
#   5. Keeps the account's user services running without a login session
#      (loginctl enable-linger), then installs and starts speakeasyd as a user
#      service for that account.
#   6. Waits up to a minute for the agent to enroll the machine.
set -eu

managed_config=/etc/speakeasy/managed.json
installer_url=@@INSTALLER_URL@@
control_plane_url=@@CONTROL_PLANE_URL@@

die() {
	echo "speakeasy-install: $*" >&2
	exit 1
}

target_user=""
force=0
while [ $# -gt 0 ]; do
	case "$1" in
	--user)
		[ $# -ge 2 ] || die "--user needs a value"
		target_user="$2"
		shift 2
		;;
	--user=*)
		target_user="${1#*=}"
		shift
		;;
	--force)
		force=1
		shift
		;;
	*) die "unknown argument: $1 (accepted: --user NAME, --force)" ;;
	esac
done

# 1. Platform and privileges.
[ "$(uname -s)" = "Linux" ] || die "v1 supports Linux only"
[ "$(id -u)" -eq 0 ] || die "run this as root, with sudo"
command -v loginctl >/dev/null 2>&1 || die "systemd (loginctl) is required to run the agent as a user service"

# 2. The account the agent runs as.
if [ -z "$target_user" ]; then
	target_user="${SUDO_USER:-}"
fi
if [ -z "$target_user" ]; then
	die "name the account that runs this machine's AI tools: run with sudo from that account, or pass --user NAME. The agent must run as that account, because the AI tools get their MCP tokens from it and it writes their configuration."
fi
id -u "$target_user" >/dev/null 2>&1 || die "no such account: $target_user"
target_uid="$(id -u "$target_user")"
target_gid="$(id -g "$target_user")"
if [ "$target_uid" -eq 0 ]; then
	die "the agent must not run as root. Pass --user NAME for the account that runs this machine's AI tools: they get their MCP tokens from the agent, and it writes their configuration."
fi
target_home="$(getent passwd "$target_user" 2>/dev/null | cut -d: -f6 || true)"
if [ -z "$target_home" ]; then
	target_home="$(awk -F: -v u="$target_user" '$1 == u { print $6 }' /etc/passwd)"
fi
if [ -z "$target_home" ] || [ ! -d "$target_home" ]; then
	die "cannot find the home directory of $target_user"
fi

if [ -e "$managed_config" ] && [ "$force" -ne 1 ]; then
	die "$managed_config already exists. Re-run with --force to replace it; the current file is backed up first."
fi

# 3. Install speakeasyd.
echo "Installing speakeasyd to /usr/local/bin..."
curl -fsSL "$installer_url" | sh -s -- --install-dir /usr/local/bin

# 4. Managed config. Written with a private umask to a temporary file, then
# installed with its final owner and mode in one step.
umask 077
managed_tmp="$(mktemp)"
trap 'rm -f "$managed_tmp"' EXIT
cat >"$managed_tmp" <<'@@DELIMITER@@'
@@MANAGED_CONFIG@@
@@DELIMITER@@
if [ -e "$managed_config" ]; then
	backup="$managed_config.bak.$(date +%Y%m%d%H%M%S)"
	cp -p "$managed_config" "$backup"
	echo "Backed up the existing managed config to $backup."
fi
install -d -o root -g root -m 0755 /etc/speakeasy
install -D -o root -g "$target_gid" -m 0640 "$managed_tmp" "$managed_config"
rm -f "$managed_tmp"
echo "Wrote $managed_config."

# 5. Run the agent as a user service of the target account.
loginctl enable-linger "$target_user"
runtime_dir="/run/user/$target_uid"
waited=0
while [ ! -d "$runtime_dir" ] && [ "$waited" -lt 10 ]; do
	sleep 1
	waited=$((waited + 1))
done
[ -d "$runtime_dir" ] || die "$runtime_dir did not appear after enabling lingering for $target_user"

as_target_user() {
	if command -v runuser >/dev/null 2>&1; then
		runuser -u "$target_user" -- env HOME="$target_home" XDG_RUNTIME_DIR="$runtime_dir" DBUS_SESSION_BUS_ADDRESS="unix:path=$runtime_dir/bus" "$@"
	else
		sudo -u "$target_user" env HOME="$target_home" XDG_RUNTIME_DIR="$runtime_dir" DBUS_SESSION_BUS_ADDRESS="unix:path=$runtime_dir/bus" "$@"
	fi
}

if as_target_user /usr/local/bin/speakeasyd -service install; then
	as_target_user /usr/local/bin/speakeasyd -service start
else
	echo "The speakeasyd service is already installed for $target_user; restarting it."
	as_target_user /usr/local/bin/speakeasyd -service restart
fi

# 6. Wait for enrollment. The agent records the subject the control plane
# gave this machine in its device key file; only that field is read here.
device_key="${target_home}/.local/state/speakeasy/device_key.json"
subject=""
waited=0
while [ "$waited" -lt 60 ]; do
	if [ -f "$device_key" ]; then
		subject="$(sed -n 's/.*"subject"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$device_key" | head -n 1)"
		[ -n "$subject" ] && break
	fi
	sleep 2
	waited=$((waited + 2))
done

if [ -n "$subject" ]; then
	echo "Enrolled. This machine is $subject."
else
	echo "Installed; check the dashboard at $control_plane_url to confirm the machine enrolled."
fi
`
