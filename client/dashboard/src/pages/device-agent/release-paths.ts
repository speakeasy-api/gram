/**
 * Where the device agent comes from and where it reads its managed config.
 * Shared so the device agent setup page and the agent identity connect screen
 * cannot drift into quoting different locations.
 */

// Public, unauthenticated bucket the release pipeline publishes to. The
// manifest (releases.json) lists the current version + per-platform URLs;
// binaries live under v{version}/.
export const RELEASES_BASE =
  "https://storage.googleapis.com/speakeasy-device-agent-releases-prod";

export const MANIFEST_URL = `${RELEASES_BASE}/releases.json`;

// The sandbox installer: resolves the latest release, verifies its checksum and
// installs speakeasyd. For headless Linux machines only; laptops and fleets
// install the signed .pkg / .msi.
export const SANDBOX_INSTALL_SCRIPT_URL = `${RELEASES_BASE}/install.sh`;

export const MANAGED_CONFIG_PATHS = [
  {
    os: "macOS",
    path: "/Library/Application Support/Speakeasy/managed.json",
    owner: "root",
  },
  { os: "Linux", path: "/etc/speakeasy/managed.json", owner: "root" },
  {
    os: "Windows",
    path: "%ProgramData%\\Speakeasy\\managed.json",
    owner: "SYSTEM",
  },
] as const;

// The systemd system unit for an unattended host, pinned to the account the AI
// tools run as (`speakeasyd -service install` only writes a per-user unit). Its
// header documents the install steps; the agent connect screen points at it.
export const LINUX_SYSTEM_UNIT_URL =
  "https://github.com/speakeasy-api/device-agent/blob/main/installer/linux/com.speakeasy.daemon.service";
