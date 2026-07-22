import { CodeBlock } from "@/components/code";
import { Page } from "@/components/page-layout";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Link as ExternalLink } from "@/components/ui/link";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useOrganization } from "@/contexts/Auth";
import { useAgentToken } from "@/hooks/useAgentToken";
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, ChevronRight, Download } from "lucide-react";
import React, { useEffect, useState } from "react";
import { Link } from "react-router";

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

const OS_ORDER: OsKey[] = ["macos", "windows", "linux"];

// {bash,ps}VersionAssign return the shell line that sets VERSION for the
// download snippets. When we've resolved the latest release from the manifest
// we inline it (a concrete, copy-and-run value); otherwise we fall back to a
// self-resolving one-liner so the snippet still works before the fetch lands
// or if it fails.
function bashVersionAssign(version: string | null) {
  return version
    ? `VERSION=${version}`
    : `VERSION=$(curl -s ${MANIFEST_URL} | jq -r '.latest.speakeasyd.version')`;
}
function psVersionAssign(version: string | null) {
  return version
    ? `$VERSION = "${version}"`
    : `$VERSION = (Invoke-RestMethod ${MANIFEST_URL}).latest.speakeasyd.version`;
}

type OsSpec = {
  label: string;
  tileDesc: string;
  logo: string;
  // Per-logo size: the Apple/Windows marks fill their square viewBox
  // edge-to-edge, while Tux is a taller, non-square figure — it runs a touch
  // larger (and is object-contain'd) to sit at the same optical size without
  // distorting. Defaults to h-8 w-8.
  logoSize?: string;
  // Monochrome-black logos (the Apple mark) vanish on a dark background — flip
  // them in dark mode. The colored Windows/Tux marks must NOT be inverted.
  invertLogoInDark?: boolean;
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
};

const OS_CONFIG: Record<OsKey, OsSpec> = {
  macos: {
    label: "macOS",
    tileDesc: "Apple Silicon or Intel",
    logo: "/icons/platforms/macos.svg",
    logoSize: "h-7 w-7",
    invertLogoInDark: true,
    lang: "bash",
    archNote: (
      <>
        Apple Silicon shown — swap <code>darwin_arm64</code> for{" "}
        <code>darwin_amd64</code> on Intel.
      </>
    ),
    download: (version) => `${bashVersionAssign(version)}
BASE=${RELEASES_BASE}/v$VERSION
curl -fSL -o speakeasyd "$BASE/speakeasyd_\${VERSION}_darwin_arm64"
curl -fSL -o speakeasy  "$BASE/speakeasy_\${VERSION}_darwin_arm64"`,
    chmodMove: `chmod +x speakeasyd speakeasy
sudo mv speakeasyd speakeasy /usr/local/bin/`,
    serviceNote: (
      <>
        Installs <code>speakeasyd</code> as a LaunchAgent so it runs on login.
      </>
    ),
    serviceRegister: `speakeasyd -service install
speakeasyd -service start`,
    verify: `speakeasy status`,
    downloadKeys: ["darwin/arm64", "darwin/amd64"],
  },
  windows: {
    label: "Windows",
    tileDesc: "x64",
    logo: "/icons/platforms/windows.svg",
    logoSize: "h-7 w-7",
    lang: "powershell",
    download: (version) => `${psVersionAssign(version)}
$BASE = "${RELEASES_BASE}/v$VERSION"
Invoke-WebRequest "$BASE/speakeasyd_\${VERSION}_windows_amd64.exe" -OutFile speakeasyd.exe
Invoke-WebRequest "$BASE/speakeasy_\${VERSION}_windows_amd64.exe"  -OutFile speakeasy.exe`,
    serviceNote: (
      <>
        Installs <code>speakeasyd</code> as a Windows service.
      </>
    ),
    serviceRegister: `.\\speakeasyd.exe -service install
.\\speakeasyd.exe -service start`,
    verify: `.\\speakeasy.exe status`,
    downloadKeys: ["windows/amd64"],
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
    verify: `speakeasy status`,
    downloadKeys: ["linux/amd64", "linux/arm64"],
  },
};

function SubHeading({ children }: { children: React.ReactNode }) {
  return <Type className="mb-2 font-medium">{children}</Type>;
}

// StepNote renders the muted "why" line under a step's command block.
function StepNote({ children }: { children: React.ReactNode }) {
  return (
    <Type small muted>
      {children}
    </Type>
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
    <div className="overflow-hidden rounded-lg border">
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
// download glyph, the role (Daemon vs CLI), and the monospace filename. The
// `download` attribute makes the browser save the file rather than navigate to
// it, and the sha256 rides along as the title for verification.
function BinaryDownloadButton({
  href,
  sha256,
  role,
  name,
  version,
}: {
  href: string;
  sha256: string;
  role: string;
  name: string;
  version: string;
}) {
  return (
    <a
      href={href}
      download
      title={`sha256: ${sha256}`}
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
// alternative to the curl/PowerShell download script). Degrades to a manifest
// link if the fetch fails.
function ManualDownload({ os }: { os: OsKey }) {
  const { data, isLoading, isError } = useAgentReleases();

  if (isLoading) {
    return (
      <Type small muted>
        Loading the latest release…
      </Type>
    );
  }

  const daemon = data?.latest?.["speakeasyd"];
  const cli = data?.latest?.["speakeasy"];
  if (isError || !daemon || !cli) {
    return (
      <Type small muted>
        Couldn't load the latest release — open the{" "}
        <ExternalLink to={MANIFEST_URL} external>
          release manifest
        </ExternalLink>{" "}
        for the current version and download URLs.
      </Type>
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
      <div className="overflow-hidden rounded-md border text-sm">
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
      <Type small muted>
        Hover a button for its <code>sha256</code>. Then press{" "}
        <strong className="font-medium">Next step</strong>.
      </Type>
    </div>
  );
}

// DownloadStep is the first setup step: two ways to get the binaries (script or
// direct download), separated by an OR so it's clear they're alternatives.
function DownloadStep({ os }: { os: OsKey }) {
  const { data } = useAgentReleases();
  const version = data?.latest["speakeasyd"]?.version ?? null;
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
    <div className="border-border bg-card flex flex-col gap-2 rounded-md border p-3">
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

// ManualIdentity is the personal/PoC identity path: sign in once with the CLI.
function ManualIdentity() {
  return (
    <div className="flex flex-col gap-4">
      <Type muted>
        On a device that isn't MDM-managed, set identity by signing in once
        after installing with no <code>managed.json</code> required.
      </Type>
      <CodeBlock language="bash">{`speakeasy enroll`}</CodeBlock>
      <Type small muted>
        It opens a browser, you sign in, and the agent stores your email locally
        in <code>local.json</code>. If IT later pushes a{" "}
        <code>managed.json</code>, that takes precedence.
      </Type>
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

// FleetIdentity is the MDM identity path: deploy a managed.json so IT sets
// identity centrally. Includes inline org_token generation/rotation.
function FleetIdentity() {
  const { name: orgName, slug: orgSlug } = useOrganization();
  const apiKeysHref = useOrgRoutes().apiKeys.href();

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
              onClick={generate}
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
      <Type muted>
        On an MDM-managed device the agent reads its identity from a{" "}
        <code>managed.json</code> that IT deploys (Kandji, Jamf, Intune, ...)
        with no per-user enrollment. IT owns this file; the agent only reads it,
        and it wins over anything a user sets locally.
      </Type>

      <div>
        <SubHeading>File location</SubHeading>
        <Type small muted className="mb-3">
          Deploy the file to the fixed system path for each OS. Create the
          directory <code>0755</code> and the file <code>0640</code> (or
          equivalent ACLs on Windows). The file must be{" "}
          <strong>readable by the user the agent runs as</strong> — the agent
          runs as the logged-in user, not root. The agent only reads this file;
          it never writes it.
        </Type>
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
        <Type small muted className="mt-2">
          <code>org_slug</code> and <code>org_name</code> are pre-filled for
          this org. <code>email</code> is per-user; have your MDM substitute its
          per-user email variable (Kandji / Jamf <code>$EMAIL</code>, or your
          platform's equivalent) so one profile serves the whole fleet, or omit{" "}
          <code>email</code> and have each user run{" "}
          <code>speakeasy enroll</code>. Click{" "}
          <strong className="text-foreground">Generate token</strong> in the
          example to mint the <code>org_token</code>.
        </Type>

        <div className="mt-4 flex flex-col gap-3">
          {generatedToken && (
            <Alert variant="warning">
              <Icon name="triangle-alert" className="h-4 w-4" />
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
            <Alert variant="destructive">
              <Icon name="triangle-alert" className="h-4 w-4" />
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
        <Type small muted>
          Package <code>managed.json</code> as a custom configuration profile
          that drops the file at the path above with the right permissions, then
          scope it to your target device groups. <code>org_token</code> is a
          credential — distribute it the way you'd distribute any API key, and
          don't commit it or paste it into chat. If the agent isn't picking up
          the file, confirm the path with <code>speakeasy config path</code>,
          check that it's readable by the logged-in user, and validate the JSON.
        </Type>
      </div>
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

// IdentityStep is the final sheet step: pick how the agent learns who's on the
// device (fleet MDM vs personal enrollment).
function IdentityStep() {
  return (
    <div className="flex flex-col gap-6">
      <Type small muted>
        How the agent learns who's on the device. Fleet is the recommended path
        for an org; personal enrollment is handy for testing.
      </Type>
      <Tabs defaultValue="fleet" className="gap-6">
        <TabsList className="grid h-auto w-full grid-cols-2 items-stretch gap-3 bg-transparent p-0">
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
            desc="Sign in once with speakeasy enroll."
          />
        </TabsList>
        <TabsContent value="fleet" className="pt-2">
          <FleetIdentity />
        </TabsContent>
        <TabsContent value="personal" className="pt-2">
          <ManualIdentity />
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
      className="border-border data-[state=active]:border-primary/40 h-auto flex-col items-start justify-start gap-1 rounded-md border p-4 text-left whitespace-normal"
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

// buildSteps assembles the ordered setup steps for an OS. Windows has no
// chmod/move step, so the list length (and numbering) varies by OS.
function buildSteps(os: OsKey): SetupStep[] {
  const cfg = OS_CONFIG[os];
  const steps: SetupStep[] = [
    { title: "Download the binaries", body: <DownloadStep os={os} /> },
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
  steps.push({ title: "Set the user's identity", body: <IdentityStep /> });
  return steps;
}

// DeviceAgentSetupSheet walks through the per-OS setup as a sequence of steps,
// matching the platform-instrumentation sheet used elsewhere in onboarding:
// progress dots up top, one step visible at a time, back/next in the footer.
function DeviceAgentSetupSheet({
  os,
  open,
  onOpenChange,
}: {
  os: OsKey | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [stepIdx, setStepIdx] = useState(0);

  // Reset to the first step whenever a fresh OS is opened.
  useEffect(() => {
    if (open) setStepIdx(0);
  }, [open, os]);

  const steps = os ? buildSteps(os) : [];
  const cfg = os ? OS_CONFIG[os] : null;
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
            Install the Speakeasy device agent on {cfg?.label}
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
                "h-1 rounded-full transition-all",
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

// OsTile is a clickable platform card, styled like the agent-platform tiles in
// the manual instrumentation step. Clicking it opens the setup sheet.
function OsTile({ os, onClick }: { os: OsKey; onClick: () => void }) {
  const cfg = OS_CONFIG[os];
  return (
    <button
      type="button"
      onClick={onClick}
      className="border-border bg-card hover:border-foreground/20 flex w-full items-center gap-4 rounded-lg border p-4 text-left transition-all"
    >
      <div className="bg-secondary flex h-14 w-14 flex-shrink-0 items-center justify-center rounded-lg">
        <img
          src={cfg.logo}
          alt={`${cfg.label} logo`}
          className={cn(
            cfg.logoSize ?? "h-8 w-8",
            "object-contain",
            cfg.invertLogoInDark && "dark:invert",
          )}
        />
      </div>
      <div className="min-w-0 flex-1 space-y-1">
        <p className="text-foreground text-sm font-medium">{cfg.label}</p>
        <p className="text-muted-foreground text-xs">{cfg.tileDesc}</p>
      </div>
      <ChevronRight className="text-muted-foreground h-4 w-4 flex-shrink-0" />
    </button>
  );
}

// DeviceAgentSetup is the shared device-agent setup UI: pick your OS from the
// tile grid, then walk the per-OS install + identity steps in a sheet. Rendered
// both on the standalone Device Agent page and inside the onboarding
// "Instrument agents" step (Device Agent tab), so setup lives in one place.
export function DeviceAgentSetup(): React.JSX.Element {
  const [sheetOs, setSheetOs] = useState<OsKey | null>(null);

  return (
    <Page.Section>
      <Page.Section.Title>Install the agent</Page.Section.Title>
      <Page.Section.Description>
        The Speakeasy device agent runs on-device and enforces your org's
        required AI-tool plugins and MCP configuration, then reports compliance
        back to Speakeasy.
      </Page.Section.Description>
      <Page.Section.Body>
        <div className="flex flex-col gap-4">
          <Alert variant="info">
            <Icon name="building-2" className="h-4 w-4" />
            <AlertTitle>Rolling out to more than a few machines?</AlertTitle>
            <AlertDescription>
              We recommend deploying the agent through your MDM (Kandji, Jamf,
              Intune, or similar). It installs the binaries and drops a{" "}
              <code>managed.json</code> so identity and enrollment are set
              centrally — no per-user setup. The{" "}
              <strong className="font-medium">Fleet (MDM)</strong> path in each
              platform's walkthrough covers it.
            </AlertDescription>
          </Alert>
          <Type small muted>
            Pick the platform you're installing on to walk through setup.
          </Type>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            {OS_ORDER.map((os) => (
              <OsTile key={os} os={os} onClick={() => setSheetOs(os)} />
            ))}
          </div>

          {/* Sheet must live inside Page.Section.Body: Page.Section only
              renders its recognized slot children (Title/Description/Body/CTA)
              and drops anything else, so a Sheet placed as a direct Section
              child never mounts. */}
          <DeviceAgentSetupSheet
            os={sheetOs}
            open={sheetOs !== null}
            onOpenChange={(open) => {
              if (!open) setSheetOs(null);
            }}
          />
        </div>
      </Page.Section.Body>
    </Page.Section>
  );
}
