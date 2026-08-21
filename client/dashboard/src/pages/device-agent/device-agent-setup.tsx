import { CodeBlock } from "@/components/code";
import { Page } from "@/components/page-layout";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Dialog } from "@/components/ui/Dialog";
import { Link as ExternalLink } from "@/components/ui/Link";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useAgentToken } from "@/hooks/useAgentToken";
import { cn, getServerURL } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Download } from "lucide-react";
import React, { useEffect, useState } from "react";
import { Link } from "react-router";
import {
  RemoteNetworkAccessStep,
  RemoteOrganizationDefaultStep,
  RemoteSetupScriptStep,
} from "./device-agent-cloud-setup";

// Public, unauthenticated bucket the release pipeline publishes to. The
// manifest (releases.json) lists the current version + per-platform URLs;
// binaries live under v{version}/.
const RELEASES_BASE =
  "https://storage.googleapis.com/speakeasy-device-agent-releases-prod";
const MANIFEST_URL = `${RELEASES_BASE}/releases.json`;

// Shared inline-link styling for the anchors/Links on this page.
const LINK_CLASS = "underline underline-offset-2 hover:text-foreground";

type ReleaseArtifact = {
  goos: string;
  goarch: string;
  url: string;
  sha256: string;
  size: number;
};
type ReleaseBinary = { version: string; artifacts: ReleaseArtifact[] };
type ReleasesManifest = { latest: Record<string, ReleaseBinary> };

// useAgentReleases fetches the public release manifest so we can render direct
// download links. A browser fetch (unlike the curl steps) needs CORS on the
// bucket — enabled in gram-infra. When the fetch fails (CORS not yet deployed,
// offline) the manual-download list falls back to a link to the raw manifest.
function useAgentReleases() {
  return useQuery<ReleasesManifest>({
    queryKey: ["device-agent-releases"],
    queryFn: async () => {
      const res = await fetch(MANIFEST_URL, {
        headers: { Accept: "application/json" },
      });
      if (!res.ok) throw new Error(`release manifest: HTTP ${res.status}`);
      return res.json() as Promise<ReleasesManifest>;
    },
    staleTime: 5 * 60 * 1000,
    retry: 1,
  });
}

const PLATFORM_LABELS: Record<string, string> = {
  "darwin/arm64": "macOS · Apple Silicon",
  "darwin/amd64": "macOS · Intel",
  "windows/amd64": "Windows · x64",
  "linux/amd64": "Linux · x64",
  "linux/arm64": "Linux · arm64",
};
const platformKey = (a: { goos: string; goarch: string }) =>
  `${a.goos}/${a.goarch}`;

// ---------------------------------------------------------------------------
// OS is picked from the tile grid up front, then threaded into the setup sheet,
// so a reader only ever sees the commands + download links for their platform.
// All the per-OS specifics live in this one table.
// ---------------------------------------------------------------------------
type OsKey = "macos" | "windows" | "linux";
type PlatformKey = OsKey | "remote";

const PLATFORM_ORDER: PlatformKey[] = ["macos", "windows", "linux", "remote"];

// A manifest-supplied version is rendered directly into a copy-paste
// shell/PowerShell snippet. Quoting alone doesn't make that safe — double
// quotes still expand $(...) — so validate shape instead of trying to escape
// arbitrary content; same charset tag-internal.yml (device-agent) requires
// when minting a release tag.
const VERSION_PATTERN =
  /^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$/;
function safeVersion(version: string | null | undefined) {
  return version && VERSION_PATTERN.test(version) ? version : null;
}

// {bash,ps}VersionAssign return the shell line that sets VERSION for the
// download snippets. When we've resolved the latest release from the manifest
// we inline it (a concrete, copy-and-run value); otherwise we fall back to a
// self-resolving one-liner so the snippet still works before the fetch lands
// or if it fails.
//
// The manifest value is fetched at runtime and pasted by the user into a
// (often sudo-adjacent) shell, so only a strictly semver-shaped version is
// ever inlined — anything else (e.g. a tampered manifest smuggling `$(...)`)
// takes the fallback path instead of landing in the snippet.
const INLINABLE_VERSION = /^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$/;
function inlinableVersion(version: string | null) {
  return version !== null && INLINABLE_VERSION.test(version) ? version : null;
}
function bashVersionAssign(version: string | null) {
  version = inlinableVersion(version);
  return version
    ? `VERSION="${version}"`
    : `VERSION=$(curl -s ${MANIFEST_URL} | jq -r '.latest.speakeasyd.version')`;
}
function psVersionAssign(version: string | null) {
  version = inlinableVersion(version);
  return version
    ? `$VERSION = "${version}"`
    : `$VERSION = (Invoke-RestMethod ${MANIFEST_URL}).latest.speakeasyd.version`;
}

// Fields every platform tile/sheet header needs, regardless of install method.
type BaseOsSpec = {
  label: string;
  tileDesc: string;
  logo?: string;
  // Per-logo size: the Apple/Windows marks fill their square viewBox
  // edge-to-edge, while Tux is a taller, non-square figure — it runs a touch
  // larger (and is object-contain'd) to sit at the same optical size without
  // distorting. Defaults to h-8 w-8.
  logoSize?: string;
  // Monochrome-black logos (the Apple mark) vanish on a dark background — flip
  // them in dark mode. The colored Windows/Tux marks must NOT be inverted.
  invertLogoInDark?: boolean;
};

// Linux still ships as raw binaries registered via a manual service-install
// script; macOS moved to a signed, notarized .pkg and Windows to a signed
// .msi (their install steps are bespoke components, not script fields).
// Keeping this as its own type (instead of leaving these fields optional on
// a single OsSpec) means macOS/Windows can't silently carry stale script
// fields that nothing renders anymore.
type ScriptOsSpec = BaseOsSpec & {
  lang: "bash" | "powershell";
  archNote?: React.ReactNode;
  download: (version: string | null) => string;
  // Undefined on Windows: nothing to chmod/move, the .exe runs in place.
  chmodMove?: string;
  serviceNote?: React.ReactNode;
  serviceRegister: string;
  verify: string;
  // Manifest platform keys to surface as direct-download links for this OS.
  downloadKeys: string[];
  // The OS ships a root-helper install package (.deb/.rpm on Linux) that gets
  // its own setup step. See HelperPackageStep.
  hasHelperPackage?: boolean;
};

const OS_CONFIG: {
  macos: BaseOsSpec;
  windows: BaseOsSpec;
  linux: ScriptOsSpec;
} = {
  macos: {
    label: "macOS",
    tileDesc: "Apple Silicon or Intel",
    logo: "/icons/platforms/macos.svg",
    logoSize: "h-7 w-7",
    invertLogoInDark: true,
  },
  windows: {
    label: "Windows",
    tileDesc: "x64",
    logo: "/icons/platforms/windows.svg",
    logoSize: "h-7 w-7",
  },
  linux: {
    label: "Linux",
    tileDesc: "x64 or arm64",
    logo: "/icons/platforms/linux.svg",
    logoSize: "h-9 w-9",
    lang: "bash",
    archNote: (
      <>
        amd64 shown — swap <code>linux_amd64</code> for <code>linux_arm64</code>{" "}
        on ARM.
      </>
    ),
    download: (version) => `${bashVersionAssign(version)}
BASE=${RELEASES_BASE}/v$VERSION
curl -fSL -o speakeasyd "$BASE/speakeasyd_\${VERSION}_linux_amd64"
curl -fSL -o speakeasy  "$BASE/speakeasy_\${VERSION}_linux_amd64"`,
    chmodMove: `chmod +x speakeasyd speakeasy
sudo mv speakeasyd speakeasy /usr/local/bin/`,
    serviceNote: (
      <>
        Installs <code>speakeasyd</code> as a systemd service.
      </>
    ),
    serviceRegister: `speakeasyd -service install
speakeasyd -service start`,
    verify: `speakeasyd status`,
    downloadKeys: ["linux/amd64", "linux/arm64"],
    hasHelperPackage: true,
  },
};

const REMOTE_PLATFORM_CONFIG: BaseOsSpec = {
  label: "Remote sessions",
  tileDesc: "Claude Code on the web",
};

function platformConfig(platform: PlatformKey): BaseOsSpec {
  return platform === "remote" ? REMOTE_PLATFORM_CONFIG : OS_CONFIG[platform];
}

function SubHeading({ children }: { children: React.ReactNode }) {
  return <Text className="mb-2 font-medium">{children}</Text>;
}

// StepNote renders the muted "why" line under a step's command block.
function StepNote({ children }: { children: React.ReactNode }) {
  return (
    <Text small muted>
      {children}
    </Text>
  );
}

// SubLabel is the small uppercase caption above a sub-block within a sheet step.
function SubLabel({ children }: { children: React.ReactNode }) {
  return (
    <span className="text-muted-foreground text-xs font-medium tracking-wider uppercase">
      {children}
    </span>
  );
}

// OrDivider is a labelled rule separating the two download paths (script vs
// direct download) so it's clear they're alternatives, not sequential steps.
function OrDivider() {
  return (
    <div className="flex items-center gap-3 py-1">
      <div className="bg-border h-px flex-1" />
      <span className="text-muted-foreground text-xs font-medium tracking-wider uppercase">
        or
      </span>
      <div className="bg-border h-px flex-1" />
    </div>
  );
}

function Table({
  headers,
  children,
}: {
  headers: string[];
  children: React.ReactNode;
}) {
  return (
    <div className="overflow-hidden border">
      <table className="w-full text-sm">
        <thead className="bg-muted/50 text-muted-foreground">
          <tr>
            {headers.map((h) => (
              <th key={h} className="px-4 py-2 text-left font-medium">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>{children}</tbody>
      </table>
    </div>
  );
}

// BinaryDownloadButton renders one binary as a download-affordant button: a
// download glyph, the role (Daemon vs CLI vs the macOS Installer pkg), and
// the monospace filename. The `download` attribute makes the browser save
// the file rather than navigate to it. sha256 rides along as the title for
// verification when known — the pkg isn't in releases.json, so it has none.
function BinaryDownloadButton({
  href,
  sha256,
  role,
  name,
  version,
}: {
  href: string;
  sha256?: string;
  role: string;
  name: string;
  version: string;
}) {
  return (
    <a
      href={href}
      download
      title={sha256 ? `sha256: ${sha256}` : undefined}
      className="border-border bg-card hover:border-foreground/20 hover:bg-secondary/40 flex min-w-40 items-start gap-2 rounded-md border px-3 py-2 transition-colors"
    >
      <Download className="text-muted-foreground mt-0.5 h-3.5 w-3.5 shrink-0" />
      <span className="flex flex-col leading-tight">
        <span className="text-muted-foreground text-[10px] font-medium tracking-wider uppercase">
          {role}
        </span>
        <span className="text-foreground font-mono text-xs">{name}</span>
        <span className="text-muted-foreground mt-0.5 text-[10px]">
          v{version}
        </span>
      </span>
    </a>
  );
}

// ManualDownload lists the direct binary links for the selected OS only (the
// alternative to the curl download script). Degrades to a manifest link if
// the fetch fails. Linux only — macOS installs from the pkg (MacInstallStep)
// and Windows from the msi (WinInstallStep), not the raw-binary manifest.
function ManualDownload({ os }: { os: "linux" }) {
  const { data, isLoading, isError } = useAgentReleases();

  if (isLoading) {
    return (
      <Text small muted>
        Loading the latest release…
      </Text>
    );
  }

  const daemon = data?.latest?.["speakeasyd"];
  const cli = data?.latest?.["speakeasy"];
  if (isError || !daemon || !cli) {
    return (
      <Text small muted>
        Couldn't load the latest release — open the{" "}
        <ExternalLink
          href={MANIFEST_URL}
          target="_blank"
          iconSuffixName="external-link"
        >
          release manifest
        </ExternalLink>{" "}
        for the current version and download URLs.
      </Text>
    );
  }

  const cfg = OS_CONFIG[os];
  const artifactFor = (bin: ReleaseBinary, key: string) =>
    bin.artifacts.find((a) => platformKey(a) === key);
  const keys = OS_CONFIG[os].downloadKeys.filter((key) =>
    daemon.artifacts.some((a) => platformKey(a) === key),
  );

  return (
    <div className="flex flex-col gap-2">
      <div className="overflow-hidden border text-sm">
        {keys.map((key) => {
          const d = artifactFor(daemon, key);
          const c = artifactFor(cli, key);
          return (
            <div
              key={key}
              className="flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3 last:border-b-0"
            >
              <span className="text-muted-foreground flex items-center gap-2 text-sm">
                <img
                  src={cfg.logo}
                  alt=""
                  aria-hidden
                  className={cn(
                    "h-4 w-4 shrink-0 object-contain",
                    cfg.invertLogoInDark && "dark:invert",
                  )}
                />
                {PLATFORM_LABELS[key] ?? key}
              </span>
              <div className="flex flex-wrap gap-2">
                {d && (
                  <BinaryDownloadButton
                    href={d.url}
                    sha256={d.sha256}
                    role="Daemon"
                    name="speakeasyd"
                    version={daemon.version}
                  />
                )}
                {c && (
                  <BinaryDownloadButton
                    href={c.url}
                    sha256={c.sha256}
                    role="CLI"
                    name="speakeasy"
                    version={cli.version}
                  />
                )}
              </div>
            </div>
          );
        })}
      </div>
      <Text small muted>
        Hover a button for its <code>sha256</code>. Then press{" "}
        <strong className="font-medium">Next step</strong>.
      </Text>
    </div>
  );
}

// DownloadStep is the first setup step on Linux: two ways to get the
// binaries (script or direct download), separated by an OR so it's clear
// they're alternatives. macOS uses MacInstallStep and Windows WinInstallStep
// instead.
function DownloadStep({ os }: { os: "linux" }) {
  const { data } = useAgentReleases();
  const version = safeVersion(data?.latest?.["speakeasyd"]?.version);
  const cfg = OS_CONFIG[os];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <SubLabel>Tooling breakdown</SubLabel>
        <BinaryLegend />
      </div>
      <div className="flex flex-col gap-2">
        <SubLabel>Run the download script</SubLabel>
        {cfg.archNote && <StepNote>{cfg.archNote}</StepNote>}
        <CodeBlock language={cfg.lang}>{cfg.download(version)}</CodeBlock>
      </div>
      <OrDivider />
      <div className="flex flex-col gap-2">
        <SubLabel>Download the binaries directly</SubLabel>
        <ManualDownload os={os} />
      </div>
    </div>
  );
}

// BinaryLegend explains the two binaries the download steps fetch, since their
// names differ by a single character (speakeasyd vs speakeasy) but they play
// different roles.
function BinaryLegend() {
  return (
    <div className="border-border bg-card flex flex-col gap-2 border p-3">
      <div className="grid grid-cols-[auto_1fr] items-baseline gap-x-3 gap-y-1.5">
        <code className="text-foreground font-mono text-xs">speakeasyd</code>
        <span className="text-muted-foreground text-xs">
          The background <strong className="font-medium">daemon</strong> — runs
          as a service and does the enforcement.
        </span>
        <code className="text-foreground font-mono text-xs">speakeasy</code>
        <span className="text-muted-foreground text-xs">
          The <strong className="font-medium">CLI</strong> — for status checks
          and enrollment.
        </span>
      </div>
    </div>
  );
}

// HelperPackageStep is the Linux-only step for the speakeasy-helper root
// helper. The daemon runs as the logged-in user and can't write the root-owned
// managed config layer itself, so enforcement of "managed" tools needs the
// helper installed as a systemd system service — which ships as a .deb/.rpm
// (the Linux analog of the macOS .pkg). The packages are mirrored to the same
// public bucket as the binaries but are deliberately NOT in the release
// manifest (a root binary updates only via a package push, never the
// user-context auto-updater), so the URLs are built from the resolved version
// rather than read from manifest artifacts.
function HelperPackageStep() {
  const { data } = useAgentReleases();
  const version = data?.latest["speakeasyd"]?.version ?? null;

  // The package installs as root, so the snippet is hardened where the
  // user-context download script isn't: ARCH is detected at run time (uname -m
  // mapped to the release's amd64/arm64 naming) instead of hand-edited, and
  // the sha256 is checked against the release's published checksums.txt with
  // the install chained behind the check — a missing or tampered package never
  // reaches the package manager.
  const script = (fmt: "deb" | "rpm", install: string) =>
    `${bashVersionAssign(version)}
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
BASE=${RELEASES_BASE}/v$VERSION
PKG="speakeasy-helper_\${VERSION}_linux_\${ARCH}.${fmt}"
curl -fSLO "$BASE/$PKG"
curl -fsSL "$BASE/checksums.txt" | grep " $PKG$" | sha256sum -c - &&
  ${install}`;

  // Sits above each snippet, matching the archNote convention in DownloadStep.
  const verifyNote = (
    <StepNote>
      Detects your architecture and verifies the package's <code>sha256</code>{" "}
      against the release's <code>checksums.txt</code> before installing.
    </StepNote>
  );

  return (
    <div className="flex flex-col gap-6">
      <Text muted>
        The daemon runs as the logged-in user and can't write root-owned config,
        so enforcing tools your org marks{" "}
        <strong className="font-medium">managed</strong> needs the{" "}
        <code>speakeasy-helper</code> package: it installs a privileged writer
        as a systemd system service. Without it the agent still runs and
        reports, but can't enforce the managed layer.
      </Text>
      <div className="flex flex-col gap-2">
        <SubLabel>Debian / Ubuntu (.deb)</SubLabel>
        {verifyNote}
        <CodeBlock language="bash">
          {script("deb", `sudo apt install "./$PKG"`)}
        </CodeBlock>
      </div>
      <OrDivider />
      <div className="flex flex-col gap-2">
        <SubLabel>RHEL / Fedora (.rpm)</SubLabel>
        {verifyNote}
        <CodeBlock language="bash">
          {script("rpm", `sudo rpm -i "$PKG"`)}
        </CodeBlock>
      </div>
      <Text small muted>
        Verify with <code>systemctl status com.speakeasy.helper</code>. The
        helper is deliberately outside the agent's auto-update channel — it
        updates only via a package push (apt/dnf upgrade or your config
        management).
      </Text>
    </div>
  );
}

// ManualIdentity is the personal/PoC identity path: sign in once with the CLI.
function ManualIdentity({ os }: { os: OsKey }) {
  // macOS and Windows: bare `speakeasy` is command-not-found — neither the
  // pkg nor the msi puts the CLI on PATH (see MacVerifyStep for the macOS
  // rationale), so invoke it by its install path. Linux keeps the bare
  // command: the raw binaries move into /usr/local/bin per the install steps.
  const commands: Record<OsKey, string> = {
    macos: `"$HOME/Library/Application Support/Speakeasy/bin/speakeasy" enroll`,
    windows: `& "C:\\Program Files\\Speakeasy\\speakeasy.exe" enroll`,
    linux: "speakeasy enroll",
  };
  const command = commands[os];

  return (
    <div className="flex flex-col gap-4">
      <Text muted>
        On a device that isn't MDM-managed, set identity by signing in once
        after installing with no <code>managed.json</code> required.
      </Text>
      <CodeBlock language="bash">{command}</CodeBlock>
      <Text small muted>
        It opens a browser, you sign in, and the agent stores your email locally
        in <code>local.json</code>. If IT later pushes a{" "}
        <code>managed.json</code>, that takes precedence.
      </Text>
    </div>
  );
}

// Sentinel JSON value for org_token until one is generated. CodeBlock matches
// the token shiki emits for this value and swaps it for an inline "Generate"
// button (see the slots wiring in FleetIdentity).
const ORG_TOKEN_SENTINEL = "__SLOT_orgToken__";

// GenerateInlineButton is a compact button sized to sit inline in the code, in
// place of the org_token value.
function GenerateInlineButton({
  onClick,
  pending,
  disabled,
  existing,
}: {
  onClick: () => void;
  pending: boolean;
  disabled?: boolean;
  existing: boolean;
}) {
  const label = existing ? "Rotate token" : "Generate token";
  const pendingLabel = existing ? "Rotating…" : "Generating…";
  return (
    <Button
      variant="secondary"
      size="sm"
      onClick={onClick}
      disabled={pending || disabled}
      title={
        disabled
          ? "Generating an agent token requires the org:admin role."
          : existing
            ? "An agent token already exists — this rotates your existing tokens and adds the new token into managed.json."
            : undefined
      }
      className="-my-1 inline-flex h-6 items-center px-2 py-0 align-middle text-xs"
    >
      <Button.LeftIcon>
        <Icon name="key-round" className="h-3 w-3" />
      </Button.LeftIcon>
      <Button.Text>{pending ? pendingLabel : label}</Button.Text>
    </Button>
  );
}

// ConfigurationProfileNote surfaces the macOS-preferred alternative to the
// script-dropped managed.json below: a native Configuration Profile (Custom
// Settings payload) targeting the com.speakeasy.agent preference domain,
// materialized by the OS at /Library/Managed Preferences/com.speakeasy.agent.plist.
// Fields mirror managed.json's identity subset (no org_name/auto_update — MDM
// tooling generally handles naming/update policy itself for profiles). Both
// delivery methods work; if both are present, the profile wins per field.
function ConfigurationProfileNote() {
  return (
    <div className="bg-secondary/30 border-border rounded-md border p-4">
      <SubHeading>Configuration Profile (preferred on macOS)</SubHeading>
      <Text small muted className="mt-1 mb-3">
        Most macOS MDMs can deliver identity as a native Configuration Profile
        instead of dropping a file — no script needed. Create a Custom Settings
        payload for the preference domain <code>com.speakeasy.agent</code>; the
        OS materializes it at{" "}
        <code>/Library/Managed Preferences/com.speakeasy.agent.plist</code>.
      </Text>
      <CodeBlock language="xml">{`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
"http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>v</key><integer>1</integer>
  <key>email</key><string>jane.doe@example.com</string>
  <key>org_token</key><string>spk_org_…</string>
  <key>org_slug</key><string>example-corp</string>
</dict>
</plist>`}</CodeBlock>
      <Text small muted className="mt-2">
        Same fields as <code>managed.json</code> below, minus{" "}
        <code>org_name</code>/<code>auto_update</code>. The script-dropped file
        (below) still works on macOS too — use whichever your MDM supports more
        easily.
      </Text>
    </div>
  );
}

// FleetIdentity is the MDM identity path: deploy a managed.json so IT sets
// identity centrally. Includes inline org_token generation/rotation. On
// macOS, a native Configuration Profile is also available and preferred
// over the script-dropped managed.json (see ConfigurationProfileNote) —
// both work, and the profile wins per field if both are present.
function FleetIdentity({ os }: { os: OsKey }) {
  const { name: orgName, slug: orgSlug } = useOrganization();
  const apiKeysHref = useOrgRoutes().apiKeys.href();
  const [rotateConfirmOpen, setRotateConfirmOpen] = useState(false);

  // org_slug / org_name are org-level constants, safe to prefill. email is
  // per-user: this fleet-wide file must not pin one identity, so the example
  // shows an MDM substitution placeholder ($EMAIL) rather than the viewing
  // admin's address — the MDM swaps it per device, or it's omitted and the user
  // enrolls manually.
  const buildManagedJson = (orgToken: string) =>
    JSON.stringify(
      {
        v: 1,
        email: "$EMAIL",
        org_token: orgToken,
        org_slug: orgSlug || "acme-corp",
        org_name: orgName || "Acme Corporation",
        auto_update: "notify",
      },
      null,
      2,
    );

  // Mint/rotate the org_token (an agent-scoped key) and copy the ready-to-paste
  // managed.json on success. See useAgentToken for the create→revoke ordering.
  const {
    generatedToken,
    autoCopied,
    isPending,
    isError,
    canGenerate,
    hasExistingAgentKey,
    generate,
  } = useAgentToken({ buildCopyText: buildManagedJson });

  // org_token starts as a sentinel that CodeBlock renders as an inline
  // "generate" button; once minted we splice the real key in (returned once).
  const exampleManagedJson = buildManagedJson(
    generatedToken ?? ORG_TOKEN_SENTINEL,
  );

  const handleGenerateOrRotate = () => {
    if (hasExistingAgentKey) {
      setRotateConfirmOpen(true);
      return;
    }

    generate();
  };

  const confirmRotation = () => {
    setRotateConfirmOpen(false);
    generate();
  };

  // Host the inline action only while no token exists. CodeBlock matches the
  // sentinel as a substring of whatever token shiki emits (it ends up quoted as
  // a JSON value), so we key by the bare sentinel; copyText keeps a
  // copied-but-ungenerated file valid.
  const slots = generatedToken
    ? undefined
    : {
        [ORG_TOKEN_SENTINEL]: {
          node: (
            <GenerateInlineButton
              onClick={handleGenerateOrRotate}
              pending={isPending}
              disabled={!canGenerate}
              existing={hasExistingAgentKey}
            />
          ),
          copyText: "spk_org_REPLACE_ME",
        },
      };

  return (
    <div className="flex flex-col gap-8">
      <Text muted>
        On an MDM-managed device the agent reads its identity from a{" "}
        <code>managed.json</code> that IT deploys (Jamf, Iru (formerly Kandji),
        Intune, ...) with no per-user enrollment. IT owns this file; the agent
        only reads it, and it wins over anything a user sets locally.
      </Text>

      {os === "macos" && <ConfigurationProfileNote />}

      <div>
        <SubHeading>File location</SubHeading>
        <Text small muted className="mb-3">
          Deploy the file to the fixed system path for each OS. Create the
          directory <code>0755</code> and the file <code>0640</code> (or
          equivalent ACLs on Windows). The file must be{" "}
          <strong>readable by the user the agent runs as</strong> — the agent
          runs as the logged-in user, not root. The agent only reads this file;
          it never writes it.
        </Text>
        <Table headers={["OS", "Path", "Owner"]}>
          {MANAGED_CONFIG_PATHS.map((row) => (
            <tr key={row.os} className="border-t">
              <td className="px-4 py-2">{row.os}</td>
              <td className="px-4 py-2 font-mono text-xs">{row.path}</td>
              <td className="px-4 py-2">{row.owner}</td>
            </tr>
          ))}
        </Table>
      </div>

      <div>
        <SubHeading>Example managed.json</SubHeading>
        <CodeBlock language="json" slots={slots}>
          {exampleManagedJson}
        </CodeBlock>
        <Text small muted className="mt-2">
          <code>org_slug</code> and <code>org_name</code> are pre-filled for
          this org. <code>email</code> is per-user; have your MDM substitute its
          per-user email variable (Jamf / Iru <code>$EMAIL</code>, or your
          platform's equivalent) so one profile serves the whole fleet, or omit{" "}
          <code>email</code> and have each user run{" "}
          <code>speakeasy enroll</code>. Click{" "}
          <strong className="text-foreground">
            {hasExistingAgentKey ? "Rotate token" : "Generate token"}
          </strong>{" "}
          in the example to mint the <code>org_token</code>.
        </Text>

        <div className="mt-4 flex flex-col gap-3">
          {generatedToken && (
            <Alert variant="warning">
              <AlertTitle>
                {autoCopied
                  ? "managed.json copied to your clipboard"
                  : "Copy your managed.json now"}
              </AlertTitle>
              <AlertDescription>
                {autoCopied
                  ? "We've copied the full managed.json — with the new org_token — to your clipboard; paste it into your MDM profile."
                  : "The new org_token is spliced into the example above — copy the file now."}{" "}
                The <code>org_token</code> is shown only once and can't be
                retrieved again. Manage or revoke agent tokens anytime under
                Settings →{" "}
                <Link to={apiKeysHref} className={LINK_CLASS}>
                  API Keys
                </Link>
                .
              </AlertDescription>
            </Alert>
          )}

          {isError && (
            <Alert variant="error">
              <AlertTitle>Couldn't generate a token</AlertTitle>
              <AlertDescription>
                Something went wrong creating the agent token. Try again, or
                create one under Settings →{" "}
                <Link to={apiKeysHref} className={LINK_CLASS}>
                  API Keys
                </Link>{" "}
                with the Agent scope.
              </AlertDescription>
            </Alert>
          )}
        </div>
      </div>

      <div>
        <SubHeading>Deploying via MDM</SubHeading>
        <Text small muted>
          Package <code>managed.json</code> as a custom configuration profile
          that drops the file at the path above with the right permissions, then
          scope it to your target device groups. <code>org_token</code> is a
          credential — distribute it the way you'd distribute any API key, and
          don't commit it or paste it into chat. If the agent isn't picking up
          the file, confirm the path with <code>speakeasy config path</code>,
          check that it's readable by the logged-in user, and validate the JSON.
        </Text>
      </div>

      <Dialog open={rotateConfirmOpen} onOpenChange={setRotateConfirmOpen}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Rotate device agent token?</Dialog.Title>
            <Dialog.Description>
              This expires the token currently deployed in your MDM settings.
            </Dialog.Description>
          </Dialog.Header>
          <Alert variant="error">
            <AlertTitle>
              Your current MDM integration will stop working
            </AlertTitle>
            <AlertDescription>
              You must replace the existing <code>org_token</code> with the new
              token and propagate the updated configuration to every managed
              device. Until then, policy syncing to end-user devices will not
              work.
            </AlertDescription>
          </Alert>
          <Dialog.Footer>
            <Button
              variant="tertiary"
              onClick={() => setRotateConfirmOpen(false)}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button variant="destructive-primary" onClick={confirmRotation}>
              <Button.Text>Rotate token</Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </div>
  );
}

const MANAGED_CONFIG_PATHS = [
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
];

// MacInstallStep is the first (and only pre-identity) setup step on macOS.
// Unlike Windows/Linux there's no separate chmod/move or service-registration
// step — the pkg's postinstall does both.
function MacInstallStep() {
  const { data, isError } = useAgentReleases();
  const version = safeVersion(data?.latest?.["speakeasyd"]?.version);
  // The pkg ships from the same bucket/version layout as the raw binaries
  // (device-agent's release-pkg-macos job), but isn't itself listed in
  // releases.json — it's the manual/MDM on-ramp, not something the
  // auto-update client discovers — so its URL is built directly rather than
  // read off the manifest the way ManualDownload reads speakeasyd/speakeasy.
  const pkgUrl = version
    ? `${RELEASES_BASE}/v${version}/speakeasy-agent_${version}.pkg`
    : null;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <SubLabel>Tooling breakdown</SubLabel>
        <BinaryLegend />
      </div>
      <div className="flex flex-col gap-2">
        <SubLabel>Run the download + install script</SubLabel>
        <StepNote>
          Deploying via Fleet MDM? Provision <code>managed.json</code> first
          (see the identity step next) for a deterministic first start.
          Personal/PoC enrolls via OAuth instead — nothing to provision first.
        </StepNote>
        <CodeBlock language="bash">{`${bashVersionAssign(version)}
curl -fSL -o speakeasy-agent.pkg "${RELEASES_BASE}/v\${VERSION}/speakeasy-agent_\${VERSION}.pkg"
sudo installer -pkg speakeasy-agent.pkg -target /`}</CodeBlock>
      </div>
      <OrDivider />
      <div className="flex flex-col gap-2">
        <SubLabel>Download the installer directly</SubLabel>
        {pkgUrl ? (
          <BinaryDownloadButton
            href={pkgUrl}
            role="Installer"
            name="speakeasy-agent.pkg"
            version={version ?? ""}
          />
        ) : (
          <Text small muted>
            {isError
              ? "Couldn't load the latest release — use the download script above, or open the "
              : "Loading the latest release… or use the download script above, or open the "}
            <ExternalLink
              href={MANIFEST_URL}
              target="_blank"
              iconSuffixName="external-link"
            >
              release manifest
            </ExternalLink>{" "}
            for the current version.
          </Text>
        )}
      </div>
      <div className="flex flex-col gap-2">
        <SubLabel>Or push it as a fleet via MDM</SubLabel>
        <Text small muted>
          Get the pkg — the script or button above, or (if the automatic lookup
          above is down) build the URL directly from the current version in the{" "}
          <ExternalLink
            href={MANIFEST_URL}
            target="_blank"
            iconSuffixName="external-link"
          >
            release manifest
          </ExternalLink>
          :{" "}
          <code>.../v&lt;version&gt;/speakeasy-agent_&lt;version&gt;.pkg</code>.
          Upload it to your MDM (Jamf, Iru (formerly Kandji), Intune, …) as a{" "}
          <strong className="font-medium">Package</strong>, then scope a policy
          to install it once per computer — no script needed. The pkg installs
          the daemon, CLI, menu-bar UI, and privileged helper together, and its
          postinstall step registers the per-user LaunchAgents itself.
        </Text>
        <Text small muted>
          You won't need to redeploy this for every agent update — with{" "}
          <code>auto_update: "automatic"</code>, it stays current on its own
          after this first install.
        </Text>
      </div>
    </div>
  );
}

// The pkg deliberately doesn't add the CLI to PATH: it would collide with the
// Speakeasy SDK generator's own `speakeasy` on a dev machine, and pointing
// PATH at the pkg's staging copy would serve a stale binary after the first
// auto-update. Reach it by its per-user seed path instead — every macOS CLI
// invocation in this file does.
function MacVerifyStep() {
  return (
    <div className="flex flex-col gap-2">
      <StepNote>
        Run these in the enrolled user's own login session, not as root.
      </StepNote>
      <CodeBlock language="bash">{`AGENT_CLI="$HOME/Library/Application Support/Speakeasy/bin/speakeasy"

pkgutil --pkg-info com.speakeasy.agent.pkg
launchctl print "gui/$(id -u)/com.speakeasy.daemon"
"$AGENT_CLI" status`}</CodeBlock>
    </div>
  );
}

// WinInstallStep is the first (and only pre-verify) setup step on Windows:
// install from the signed .msi, which lays down the daemon, CLI, and UI under
// C:\Program Files\Speakeasy\ and registers the machine-wide LocalSystem
// service itself — no separate service-registration step. The primary snippet
// uses this Gram server's stable /v1/install URL, which 302-redirects to the
// current version's signed msi, so the copy never goes stale. Like the macOS
// pkg, the msi is deliberately not listed in releases.json (it's the
// manual/MDM on-ramp), so the direct-download URL is built from the resolved
// version rather than read off the manifest artifacts.
function WinInstallStep() {
  const { data, isError } = useAgentReleases();
  const version = safeVersion(data?.latest?.["speakeasyd"]?.version);
  const msiUrl = version
    ? `${RELEASES_BASE}/v${version}/speakeasy-agent_${version}.msi`
    : null;
  const stableMsiUrl = `${getServerURL()}/v1/install/device-agent-windows.msi`;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <SubLabel>Tooling breakdown</SubLabel>
        <BinaryLegend />
      </div>
      <div className="flex flex-col gap-2">
        <SubLabel>Run the download + install script</SubLabel>
        <StepNote>
          Run from an elevated (Administrator) PowerShell. The stable URL always
          redirects to the latest signed installer.
        </StepNote>
        <CodeBlock language="powershell">{`Invoke-WebRequest "${stableMsiUrl}" -OutFile speakeasy-agent.msi
msiexec /i speakeasy-agent.msi`}</CodeBlock>
      </div>
      <OrDivider />
      <div className="flex flex-col gap-2">
        <SubLabel>Download the installer directly</SubLabel>
        {msiUrl ? (
          <BinaryDownloadButton
            href={msiUrl}
            role="Installer"
            name="speakeasy-agent.msi"
            version={version ?? ""}
          />
        ) : (
          <Text small muted>
            {isError
              ? "Couldn't load the latest release — use the "
              : "Loading the latest release… or use the "}
            <ExternalLink href={stableMsiUrl} iconSuffixName="external-link">
              stable installer link
            </ExternalLink>
            , which always serves the current version.
          </Text>
        )}
      </div>
      <OrDivider />
      <div className="flex flex-col gap-2">
        <SubLabel>Scripted install with raw binaries</SubLabel>
        <StepNote>
          Raw binaries remain supported for scripted installs. Unlike the
          MSI&apos;s machine-wide LocalSystem service, this registers the
          service for the current user only.
        </StepNote>
        <CodeBlock language="powershell">{`${psVersionAssign(version)}
$BASE = "${RELEASES_BASE}/v$VERSION"
Invoke-WebRequest "$BASE/speakeasyd_\${VERSION}_windows_amd64.exe" -OutFile speakeasyd.exe
Invoke-WebRequest "$BASE/speakeasy_\${VERSION}_windows_amd64.exe"  -OutFile speakeasy.exe
.\\speakeasyd.exe -service install
.\\speakeasyd.exe -service start`}</CodeBlock>
      </div>
      <div className="flex flex-col gap-2">
        <SubLabel>Or push it as a fleet via MDM</SubLabel>
        <Text small muted>
          Upload the msi to Intune (or your MDM) as a{" "}
          <strong className="font-medium">Win32 / line-of-business app</strong>{" "}
          and assign it per machine — no script needed. Get it from the stable
          link above, and pair it with a <code>managed.json</code> pushed to{" "}
          <code>%ProgramData%\Speakeasy\</code> (see the identity step) so
          enrollment is set centrally.
        </Text>
        <Text small muted>
          Keep <code>auto_update: "notify"</code> on Windows fleets: the agent
          can&apos;t replace its own running binaries on Windows, so version
          bumps ship as MSI re-pushes from your MDM.
        </Text>
      </div>
    </div>
  );
}

// The MSI registers a machine-wide SCM service, so status comes from the SCM
// rather than a per-user unit. sc.exe is deliberate — bare `sc` is
// PowerShell's Set-Content alias. The CLI isn't on PATH (see ManualIdentity),
// so it's invoked by its install path; the daemon's named pipe grants
// interactive users client access, so no elevation is needed here.
function WinVerifyStep() {
  return (
    <CodeBlock language="powershell">{`sc.exe query com.speakeasy.daemon
& "C:\\Program Files\\Speakeasy\\speakeasy.exe" status`}</CodeBlock>
  );
}

// IdentityStep is the final sheet step: pick how the agent learns who's on the
// device (fleet MDM vs personal enrollment). Takes os because ManualIdentity's
// enroll command differs on macOS (see there).
function IdentityStep({ os }: { os: OsKey }) {
  return (
    <div className="flex flex-col gap-6">
      <Text small muted>
        How the agent learns who's on the device. Fleet is the recommended path
        for an org; personal enrollment is handy for testing.
      </Text>
      <Tabs defaultValue="fleet" className="gap-6">
        <TabsList className="grid h-auto w-full grid-cols-2 items-stretch gap-3 divide-x-0 border-0 bg-transparent p-0">
          <SetupTab
            value="fleet"
            icon="building-2"
            title="Fleet (MDM)"
            desc="IT sets identity centrally via managed.json."
          />
          <SetupTab
            value="personal"
            icon="user"
            title="Personal / PoC"
            desc="Sign in once with the agent CLI."
          />
        </TabsList>
        <TabsContent value="fleet" className="pt-2">
          <FleetIdentity os={os} />
        </TabsContent>
        <TabsContent value="personal" className="pt-2">
          <ManualIdentity os={os} />
        </TabsContent>
      </Tabs>
    </div>
  );
}

// SetupTab renders a bordered card that doubles as a tab trigger.
function SetupTab({
  value,
  icon,
  title,
  desc,
}: {
  value: string;
  icon: React.ComponentProps<typeof Icon>["name"];
  title: string;
  desc: React.ReactNode;
}) {
  return (
    <TabsTrigger
      value={value}
      className="border-border data-[state=active]:border-primary/40 h-auto flex-col items-start justify-start gap-1 border p-4 text-left whitespace-normal"
    >
      <div className="flex items-center gap-2">
        <Icon name={icon} className="h-4 w-4" />
        <span className="font-medium">{title}</span>
      </div>
      <span className="text-muted-foreground text-sm font-normal">{desc}</span>
    </TabsTrigger>
  );
}

type SetupStep = { title: string; body: React.ReactNode };

// buildSteps assembles the ordered setup steps for a platform. Remote sessions
// have their own cloud-environment flow; local platforms follow the OS-specific
// installation path below. macOS installs from a signed .pkg and Windows from
// a signed .msi (one combined install step each, no chmod/move or separate
// service registration); Linux still ships raw binaries via a download script,
// so the list length (and numbering) varies by OS.
function buildSteps(platform: PlatformKey): SetupStep[] {
  if (platform === "remote") {
    return [
      {
        title: "Configure the shared environment",
        body: <RemoteNetworkAccessStep />,
      },
      {
        title: "Install and configure the agent",
        body: <RemoteSetupScriptStep />,
      },
      {
        title: "Make it the organization default",
        body: <RemoteOrganizationDefaultStep />,
      },
    ];
  }

  if (platform === "macos") {
    return [
      { title: "Download and install the agent", body: <MacInstallStep /> },
      { title: "Verify it's running", body: <MacVerifyStep /> },
      {
        title: "Set the user's identity",
        body: <IdentityStep os={platform} />,
      },
    ];
  }

  if (platform === "windows") {
    return [
      { title: "Download and install the agent", body: <WinInstallStep /> },
      { title: "Verify it's running", body: <WinVerifyStep /> },
      {
        title: "Set the user's identity",
        body: <IdentityStep os={platform} />,
      },
    ];
  }

  const cfg = OS_CONFIG[platform];
  const steps: SetupStep[] = [
    {
      title: "Download the binaries",
      body: <DownloadStep os={platform} />,
    },
  ];
  if (cfg.chmodMove) {
    steps.push({
      title: "Make them executable and move into your PATH",
      body: <CodeBlock language={cfg.lang}>{cfg.chmodMove}</CodeBlock>,
    });
  }
  steps.push({
    title: "Register and start the background service",
    body: (
      <div className="flex flex-col gap-2">
        {cfg.serviceNote && <StepNote>{cfg.serviceNote}</StepNote>}
        <CodeBlock language={cfg.lang}>{cfg.serviceRegister}</CodeBlock>
      </div>
    ),
  });
  steps.push({
    title: "Verify it's running",
    body: <CodeBlock language={cfg.lang}>{cfg.verify}</CodeBlock>,
  });
  if (cfg.hasHelperPackage) {
    steps.push({
      title: "Install the root helper package",
      body: <HelperPackageStep />,
    });
  }
  steps.push({
    title: "Set the user's identity",
    body: <IdentityStep os={platform} />,
  });
  return steps;
}

// DeviceAgentSetupSheet walks through the selected platform as a sequence of
// steps, matching the platform-instrumentation sheet used elsewhere in onboarding:
// progress dots up top, one step visible at a time, back/next in the footer.
function DeviceAgentSetupSheet({
  platform,
  open,
  onOpenChange,
}: {
  platform: PlatformKey | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [stepIdx, setStepIdx] = useState(0);

  // Reset to the first step whenever a platform is opened.
  useEffect(() => {
    if (open) setStepIdx(0);
  }, [open, platform]);

  const steps = platform ? buildSteps(platform) : [];
  const cfg = platform ? platformConfig(platform) : null;
  const total = steps.length;
  const isLast = stepIdx === total - 1;

  const goToDot = (idx: number) => {
    if (idx <= stepIdx) setStepIdx(idx);
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col overflow-hidden sm:max-w-[662px]"
      >
        <SheetHeader className="sr-only">
          <SheetTitle>
            Set up the Speakeasy device agent for {cfg?.label}
          </SheetTitle>
          <SheetDescription>
            Step-by-step setup for the device agent.
          </SheetDescription>
        </SheetHeader>

        {/* Progress dots */}
        <div className="flex items-center gap-1.5 px-6 pt-6 pr-14">
          {steps.map((_, idx) => (
            <button
              key={idx}
              type="button"
              onClick={() => goToDot(idx)}
              className={cn(
                "h-1 transition-all",
                idx === stepIdx
                  ? "bg-foreground w-6"
                  : idx < stepIdx
                    ? "bg-foreground/40 hover:bg-foreground/60 w-4 cursor-pointer"
                    : "bg-border w-4",
              )}
            />
          ))}
          <span className="text-muted-foreground ml-auto text-[11px] tabular-nums">
            {Math.min(stepIdx + 1, total)}/{total}
          </span>
        </div>

        {/* Sliding steps */}
        <div className="relative flex-1 overflow-hidden">
          <div
            className="flex h-full transition-transform duration-300 ease-in-out"
            style={{ transform: `translateX(-${stepIdx * 100}%)` }}
          >
            {steps.map((step, idx) => (
              <div
                key={idx}
                className="w-full shrink-0 space-y-3 overflow-y-auto px-6 pb-4"
              >
                <p className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
                  Step {idx + 1}
                </p>
                <h4 className="text-foreground text-base font-medium">
                  {step.title}
                </h4>
                <div className="pt-1">{step.body}</div>
              </div>
            ))}
          </div>
        </div>

        {/* Footer */}
        <div className="border-border flex items-center justify-between border-t px-6 py-4">
          <Button
            variant="tertiary"
            size="sm"
            disabled={stepIdx === 0}
            onClick={() => setStepIdx((i) => Math.max(0, i - 1))}
          >
            <Button.LeftIcon>
              <ArrowLeft className="h-3 w-3" />
            </Button.LeftIcon>
            <Button.Text>Back</Button.Text>
          </Button>
          <Button
            variant="primary"
            size="sm"
            onClick={() => {
              if (isLast) onOpenChange(false);
              else setStepIdx((i) => Math.min(total - 1, i + 1));
            }}
          >
            <Button.Text>{isLast ? "Done" : "Next step"}</Button.Text>
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}

// PlatformTile is a clickable setup card: logo on top, name and subtitle
// stacked underneath, so four tiles fit a row without the text wrapping.
// Local operating systems use their platform marks; Remote sessions uses a
// cloud icon but opens the same sheet.
function PlatformTile({
  platform,
  onClick,
}: {
  platform: PlatformKey;
  onClick: () => void;
}) {
  const cfg = platformConfig(platform);
  return (
    <button
      type="button"
      onClick={onClick}
      className="border-border bg-card hover:border-foreground/20 flex w-full flex-col items-center gap-3 border p-5 text-center transition-all"
    >
      <div className="bg-secondary flex h-14 w-14 shrink-0 items-center justify-center">
        {platform === "remote" ? (
          <Icon name="cloud" className="h-7 w-7" />
        ) : (
          <img
            src={cfg.logo}
            alt={`${cfg.label} logo`}
            className={cn(
              cfg.logoSize ?? "h-8 w-8",
              "object-contain",
              cfg.invertLogoInDark && "dark:invert",
            )}
          />
        )}
      </div>
      <div className="space-y-1">
        <p className="text-foreground text-sm font-medium">{cfg.label}</p>
        <p className="text-muted-foreground text-xs">{cfg.tileDesc}</p>
      </div>
    </button>
  );
}

// DeviceAgentSetup is the shared device-agent setup UI: pick a local OS or
// Remote sessions from the tile grid, then walk its steps in a sheet. Rendered
// both on the standalone Device Agent page and inside onboarding.
export function DeviceAgentSetup(): React.JSX.Element {
  const [sheetPlatform, setSheetPlatform] = useState<PlatformKey | null>(null);

  return (
    <Page.Section>
      {/* The Device Agent page renders the area eyebrow with its own page
          title above the tab strip, so suppress the section-level one. */}
      <Page.Section.Title area="">Install the agent</Page.Section.Title>
      <Page.Section.Description>
        The Speakeasy device agent runs alongside your AI tools, enforces your
        org&apos;s required plugins and MCP configuration, and reports
        compliance back to Speakeasy.
      </Page.Section.Description>
      <Page.Section.Body>
        <div className="flex flex-col gap-4">
          <div className="border-border bg-card border p-4">
            <p className="text-eyebrow mb-2">Fleet rollout</p>
            <Text small muted>
              Rolling out to more than a few machines? We recommend deploying
              the agent through your MDM (Jamf, Iru (formerly Kandji), Intune,
              or similar). It installs the binaries and drops a{" "}
              <code>managed.json</code> so identity and enrollment are set
              centrally — no per-user setup. The{" "}
              <strong className="text-foreground font-medium">
                Fleet (MDM)
              </strong>{" "}
              path in each local platform&apos;s walkthrough covers it.
            </Text>
          </div>
          <Text small muted>
            Pick the platform you're installing on to walk through setup.
          </Text>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
            {PLATFORM_ORDER.map((platform) => (
              <PlatformTile
                key={platform}
                platform={platform}
                onClick={() => setSheetPlatform(platform)}
              />
            ))}
          </div>

          {/* Sheet must live inside Page.Section.Body: Page.Section only
              renders its recognized slot children (Title/Description/Body/CTA)
              and drops anything else, so a Sheet placed as a direct Section
              child never mounts. */}
          <DeviceAgentSetupSheet
            platform={sheetPlatform}
            open={sheetPlatform !== null}
            onOpenChange={(open) => {
              if (!open) setSheetPlatform(null);
            }}
          />
        </div>
      </Page.Section.Body>
    </Page.Section>
  );
}
