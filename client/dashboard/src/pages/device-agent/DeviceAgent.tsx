import { CodeBlock } from "@/components/code";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Link as ExternalLink } from "@/components/ui/link";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useOrganization } from "@/contexts/Auth";
import { useAgentToken } from "@/hooks/useAgentToken";
import { useOrgRoutes } from "@/routes";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { useQuery } from "@tanstack/react-query";
import React from "react";
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
// offline) DownloadAgent falls back to a link to the raw manifest.
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

// DownloadAgent renders per-platform download links for the daemon + CLI from
// the latest release manifest — what IT needs to bundle the binaries into an
// MDM payload (or grab them directly). Degrades to a manifest link on failure.
function DownloadAgent() {
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
      <Alert variant="warning">
        <Icon name="triangle-alert" className="h-4 w-4" />
        <AlertTitle>Couldn't load the latest release</AlertTitle>
        <AlertDescription>
          Open the{" "}
          <ExternalLink to={MANIFEST_URL} external>
            release manifest
          </ExternalLink>{" "}
          for the current version and per-platform download URLs.
        </AlertDescription>
      </Alert>
    );
  }

  const artifactFor = (bin: ReleaseBinary, key: string) =>
    bin.artifacts.find((a) => platformKey(a) === key);
  const platforms = Array.from(new Set(daemon.artifacts.map(platformKey))).sort(
    (a, b) => a.localeCompare(b),
  );

  return (
    <div className="flex flex-col gap-2">
      <Type small muted>
        Latest release: <code>v{daemon.version}</code>. Download both binaries
        for every platform you target.
      </Type>
      <Table headers={["Platform", "Daemon (speakeasyd)", "CLI (speakeasy)"]}>
        {platforms.map((key) => {
          const d = artifactFor(daemon, key);
          const c = artifactFor(cli, key);
          return (
            <tr key={key} className="border-t">
              <td className="px-4 py-2">{PLATFORM_LABELS[key] ?? key}</td>
              <td className="px-4 py-2">
                {d ? (
                  <a
                    href={d.url}
                    title={`sha256: ${d.sha256}`}
                    className={LINK_CLASS}
                  >
                    speakeasyd
                  </a>
                ) : (
                  "—"
                )}
              </td>
              <td className="px-4 py-2">
                {c ? (
                  <a
                    href={c.url}
                    title={`sha256: ${c.sha256}`}
                    className={LINK_CLASS}
                  >
                    speakeasy
                  </a>
                ) : (
                  "—"
                )}
              </td>
            </tr>
          );
        })}
      </Table>
      <Type small muted>
        Hover a link for its <code>sha256</code>, or read the full{" "}
        <ExternalLink to={MANIFEST_URL} external>
          manifest
        </ExternalLink>
        .
      </Type>
    </div>
  );
}

function SubHeading({ children }: { children: React.ReactNode }) {
  return <Type className="mb-2 font-medium">{children}</Type>;
}

function Step({
  n,
  title,
  children,
}: {
  n: number;
  title: string;
  children?: React.ReactNode;
}) {
  return (
    <li className="flex flex-col gap-2">
      <div className="flex items-baseline gap-2">
        <span className="bg-muted text-muted-foreground flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-xs font-medium">
          {n}
        </span>
        <Type className="font-medium">{title}</Type>
      </div>
      {children && <div className="ml-7 flex flex-col gap-2">{children}</div>}
    </li>
  );
}

// stepNote renders the muted "why" line under a step's command block.
function StepNote({ children }: { children: React.ReactNode }) {
  return (
    <Type small muted>
      {children}
    </Type>
  );
}

// SetupTab renders a bordered card that doubles as a tab trigger. The two
// setup paths (fleet vs manual) switch the panel below instead of jumping to
// an anchor.
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
      className="border-border data-[state=active]:border-primary data-[state=active]:ring-primary h-auto flex-col items-start justify-start gap-1 rounded-lg p-4 text-left whitespace-normal data-[state=active]:ring-1"
    >
      <div className="flex items-center gap-2">
        <Icon name={icon} className="h-4 w-4" />
        <span className="font-medium">{title}</span>
      </div>
      <span className="text-muted-foreground text-sm font-normal">{desc}</span>
    </TabsTrigger>
  );
}

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

// SkipIfDownloadedNote tells users they can skip the download step if they've
// already grabbed the binaries from the links above (e.g. to bundle them into
// an MDM payload).
function SkipIfDownloadedNote() {
  return (
    <StepNote>
      Already downloaded the binaries from the links above (e.g. to bundle them
      into your MDM payload)? Skip this step.
    </StepNote>
  );
}

function MacInstall({ version }: { version: string | null }) {
  return (
    <ol className="flex flex-col gap-5">
      <Step n={1} title="Download the daemon + CLI">
        <SkipIfDownloadedNote />
        <StepNote>
          Apple Silicon shown — swap <code>darwin_arm64</code> for{" "}
          <code>darwin_amd64</code> on Intel.
        </StepNote>
        <CodeBlock language="bash">{`${bashVersionAssign(version)}
BASE=${RELEASES_BASE}/v$VERSION
curl -fSL -o speakeasyd "$BASE/speakeasyd_\${VERSION}_darwin_arm64"
curl -fSL -o speakeasy  "$BASE/speakeasy_\${VERSION}_darwin_arm64"`}</CodeBlock>
      </Step>
      <Step n={2} title="Make them executable and move into your PATH">
        <CodeBlock language="bash">{`chmod +x speakeasyd speakeasy
sudo mv speakeasyd speakeasy /usr/local/bin/`}</CodeBlock>
      </Step>
      <Step n={3} title="Register and start the background service">
        <StepNote>
          Installs <code>speakeasyd</code> as a LaunchAgent so it runs on login.
        </StepNote>
        <CodeBlock language="bash">{`speakeasyd -service install
speakeasyd -service start`}</CodeBlock>
      </Step>
      <Step n={4} title="Verify it's running">
        <CodeBlock language="bash">{`speakeasy status`}</CodeBlock>
      </Step>
    </ol>
  );
}

function WindowsInstall({ version }: { version: string | null }) {
  return (
    <ol className="flex flex-col gap-5">
      <Step n={1} title="Download the daemon + CLI">
        <SkipIfDownloadedNote />
        <CodeBlock language="powershell">{`${psVersionAssign(version)}
$BASE = "${RELEASES_BASE}/v$VERSION"
Invoke-WebRequest "$BASE/speakeasyd_\${VERSION}_windows_amd64.exe" -OutFile speakeasyd.exe
Invoke-WebRequest "$BASE/speakeasy_\${VERSION}_windows_amd64.exe"  -OutFile speakeasy.exe`}</CodeBlock>
      </Step>
      <Step n={2} title="Register and start the Windows service">
        <CodeBlock language="powershell">{`.\\speakeasyd.exe -service install
.\\speakeasyd.exe -service start`}</CodeBlock>
      </Step>
      <Step n={3} title="Verify it's running">
        <CodeBlock language="powershell">{`.\\speakeasy.exe status`}</CodeBlock>
      </Step>
    </ol>
  );
}

function LinuxInstall({ version }: { version: string | null }) {
  return (
    <ol className="flex flex-col gap-5">
      <Step n={1} title="Download the daemon + CLI">
        <SkipIfDownloadedNote />
        <StepNote>
          amd64 shown — swap <code>linux_amd64</code> for{" "}
          <code>linux_arm64</code> on ARM.
        </StepNote>
        <CodeBlock language="bash">{`${bashVersionAssign(version)}
BASE=${RELEASES_BASE}/v$VERSION
curl -fSL -o speakeasyd "$BASE/speakeasyd_\${VERSION}_linux_amd64"
curl -fSL -o speakeasy  "$BASE/speakeasy_\${VERSION}_linux_amd64"`}</CodeBlock>
      </Step>
      <Step n={2} title="Make them executable and move into your PATH">
        <CodeBlock language="bash">{`chmod +x speakeasyd speakeasy
sudo mv speakeasyd speakeasy /usr/local/bin/`}</CodeBlock>
      </Step>
      <Step n={3} title="Register and start the background service">
        <StepNote>
          Installs <code>speakeasyd</code> as a systemd service.
        </StepNote>
        <CodeBlock language="bash">{`speakeasyd -service install
speakeasyd -service start`}</CodeBlock>
      </Step>
      <Step n={4} title="Verify it's running">
        <CodeBlock language="bash">{`speakeasy status`}</CodeBlock>
      </Step>
    </ol>
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

const MANAGED_CONFIG_FIELDS = [
  {
    field: "v",
    type: "integer",
    required: "yes",
    notes: "Schema version. Currently 1; the agent rejects files with v > 1.",
  },
  {
    field: "email",
    type: "string",
    required: "yes*",
    notes:
      "Per-user work email, shown in the agent UI as the enrolled identity. Have your MDM substitute it per device (e.g. $EMAIL), or omit it and let the user supply it via speakeasy enroll. *Required unless provided by manual enrollment.",
  },
  {
    field: "org_token",
    type: "string",
    required: "yes",
    notes: "Bearer token the agent uses to call Speakeasy. Treat as a secret.",
  },
  {
    field: "org_slug",
    type: "string",
    required: "no",
    notes: "Short org identifier (e.g. acme-corp). Used in URLs and IDs.",
  },
  {
    field: "org_name",
    type: "string",
    required: "no",
    notes: "Display name (e.g. Acme Corporation). Shown in the UI.",
  },
  {
    field: "auto_update",
    type: "string",
    required: "no",
    notes: '"disabled" (default), "notify", or "automatic". See below.',
  },
];

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

// InstallAgent is the shared install path: download the binaries and register
// the background service. The same steps work whether you run them by hand
// (personal) or script them in an MDM postinstall (fleet) — only identity
// differs afterward (see the "Set the user's identity" section).
function InstallAgent() {
  // Resolve the latest version once and inline it into the per-OS download
  // commands; the same value powers DownloadAgent's links. Null while loading
  // or if the fetch fails — the snippets fall back to a self-resolving line.
  const { data } = useAgentReleases();
  const version = data?.latest["speakeasyd"]?.version ?? null;

  return (
    <div className="flex flex-col gap-6">
      <Type muted>
        The agent is two binaries: <code>speakeasyd</code>, the background
        daemon that does the enforcement, and <code>speakeasy</code>, the CLI
        for status and enrollment. The steps are the same for everyone — run
        them by hand for a personal setup, or script them in your MDM payload's
        postinstall (run them in the logged-in user's context, since the agent
        runs as that user).
        {version ? (
          <>
            {" "}
            The commands pull the latest release (<code>v{version}</code>).
          </>
        ) : null}
      </Type>

      <div>
        <SubHeading>Download</SubHeading>
        <Type small muted className="mb-3">
          The install commands below fetch these for you. Grab them directly if
          you'd rather bundle the binaries into your MDM payload (
          <code>sha256</code> is available for each binary for verification
          purposes).
        </Type>
        <DownloadAgent />
      </div>

      <div>
        <SubHeading>Install and register the service</SubHeading>
        <Tabs defaultValue="macos" className="mt-2">
          <TabsList className="grid w-full max-w-md grid-cols-3">
            <TabsTrigger value="macos">macOS</TabsTrigger>
            <TabsTrigger value="windows">Windows</TabsTrigger>
            <TabsTrigger value="linux">Linux</TabsTrigger>
          </TabsList>
          <TabsContent value="macos" className="pt-4">
            <MacInstall version={version} />
          </TabsContent>
          <TabsContent value="windows" className="pt-4">
            <WindowsInstall version={version} />
          </TabsContent>
          <TabsContent value="linux" className="pt-4">
            <LinuxInstall version={version} />
          </TabsContent>
        </Tabs>
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
        after installing — no <code>managed.json</code> required.
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
            ? "An agent token already exists — this mints a fresh one to drop into managed.json."
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
        <code>managed.json</code> that IT deploys (Kandji, Jamf, Intune, …) — no
        per-user enrollment. IT owns this file; the agent only reads it, and it
        wins over anything a user sets locally.
      </Type>

      <div>
        <SubHeading>Two config layers</SubHeading>
        <Type small muted>
          The agent merges two files per field, with <code>managed.json</code>{" "}
          (IT-owned) always winning over <code>local.json</code> (written by a
          user's <code>speakeasy enroll</code>). So IT can set{" "}
          <code>org_token</code> centrally while a user's email comes from
          either layer. On a fully MDM-managed device, <code>managed.json</code>{" "}
          supplies everything and the device shows as "Provisioned by IT".
        </Type>
      </div>

      <div>
        <SubHeading>File location</SubHeading>
        <Type small muted className="mb-3">
          Deploy the file to the fixed system path for each OS. Create the
          directory <code>0755</code> and the file <code>0640</code> (or
          equivalent ACLs on Windows). The file must be{" "}
          <strong>readable by the user the agent runs as</strong> — the agent
          runs as the logged-in user, not root, so owner-only{" "}
          <code>0600 root:wheel</code> on macOS won't work; use{" "}
          <code>0640</code> with an explicit group/read ACL for the agent user.
          The agent only reads this file; it never writes it.
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
        <SubHeading>Schema</SubHeading>
        <Table headers={["Field", "Type", "Required", "Notes"]}>
          {MANAGED_CONFIG_FIELDS.map((row) => (
            <tr key={row.field} className="border-t align-top">
              <td className="px-4 py-2 font-mono text-xs">{row.field}</td>
              <td className="px-4 py-2">{row.type}</td>
              <td className="px-4 py-2">{row.required}</td>
              <td className="text-muted-foreground px-4 py-2">{row.notes}</td>
            </tr>
          ))}
        </Table>
        <Type small muted className="mt-2">
          <code>auto_update</code> controls self-update: <code>"disabled"</code>{" "}
          never checks, <code>"notify"</code> surfaces available updates without
          installing, and <code>"automatic"</code> downloads and installs them.
          For MDM fleets, <code>"notify"</code> keeps IT in control of what
          rolls out.
        </Type>
      </div>

      <div>
        <SubHeading>Example managed.json</SubHeading>
        <CodeBlock language="json" slots={slots}>
          {exampleManagedJson}
        </CodeBlock>
        <Type small muted className="mt-2">
          <code>org_slug</code> and <code>org_name</code> are pre-filled for
          this org. <code>email</code> is per-user — have your MDM substitute
          its per-user email variable (Kandji / Jamf <code>$EMAIL</code>, or
          your platform's equivalent) so one profile serves the whole fleet, or
          omit <code>email</code> and have each user run{" "}
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
        <SubHeading>Security</SubHeading>
        <ul className="text-muted-foreground flex flex-col gap-2 text-sm">
          <li>
            <code>org_token</code> is a credential — distribute it the way you'd
            distribute any API key, and don't commit it or paste it into chat.
          </li>
          <li>
            The agent never writes <code>managed.json</code> and does not log
            the token or email (PII is redacted at the logging layer).
          </li>
        </ul>
      </div>

      <div>
        <SubHeading>Deploying via MDM</SubHeading>
        <Type small muted>
          Mechanics vary by platform. The common pattern: package{" "}
          <code>managed.json</code> as a custom configuration profile that drops
          the file at the path above with the right permissions, then scope it
          to your target device groups. If the agent isn't picking up the file,
          confirm the path with <code>speakeasy config path</code>, check that
          the file is readable by the logged-in user, and validate the JSON.
        </Type>
      </div>
    </div>
  );
}

export default function DeviceAgent() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["org:read", "org:admin"]} level="page">
          <Page.Section>
            <Page.Section.Title stage="preview">
              Install the agent
            </Page.Section.Title>
            <Page.Section.Description>
              The Speakeasy device agent runs on developer laptops and enforces
              your org's required AI-tool plugins and MCP configuration, then
              reports compliance back to Speakeasy. Download the daemon + CLI
              and register the background service — the same steps work whether
              you run them by hand or script them in your MDM deployment.
            </Page.Section.Description>
            <Page.Section.Body>
              <InstallAgent />
            </Page.Section.Body>
          </Page.Section>

          <Page.Section>
            <Page.Section.Title>Set the user's identity</Page.Section.Title>
            <Page.Section.Description>
              How the agent learns who's on the device. Fleet is the recommended
              path for an org; personal enrollment is handy for testing.
            </Page.Section.Description>
            <Page.Section.Body>
              <Tabs defaultValue="fleet" className="gap-6">
                <TabsList className="grid h-auto w-full items-stretch gap-4 bg-transparent p-0 @2xl/main:grid-cols-2">
                  <SetupTab
                    value="fleet"
                    icon="building-2"
                    title="Fleet (MDM)"
                    desc="Deploy a managed.json so IT sets identity centrally — no per-user step."
                  />
                  <SetupTab
                    value="personal"
                    icon="user"
                    title="Personal / PoC"
                    desc="Each user signs in once with speakeasy enroll. Good for testing."
                  />
                </TabsList>
                <TabsContent value="fleet" className="pt-2">
                  <FleetIdentity />
                </TabsContent>
                <TabsContent value="personal" className="pt-2">
                  <ManualIdentity />
                </TabsContent>
              </Tabs>
            </Page.Section.Body>
          </Page.Section>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
