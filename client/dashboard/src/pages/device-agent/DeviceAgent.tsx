import { CodeBlock } from "@/components/code";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { Icon } from "@speakeasy-api/moonshine";
import React from "react";

// Public, unauthenticated bucket the release pipeline publishes to. The
// manifest (releases.json) lists the current version + per-platform URLs;
// binaries live under v{version}/.
const RELEASES_BASE =
  "https://storage.googleapis.com/speakeasy-device-agent-releases-prod";
const MANIFEST_URL = `${RELEASES_BASE}/releases.json`;

// Example version used in the copy/paste snippets. Operators substitute the
// current version (see the "find the version" step in each tab) — we keep a
// concrete value here rather than a literal <VERSION> so the blocks paste and
// run as-is once edited.
const EXAMPLE_VERSION = "0.1.0";

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
      {children && <div className="ml-7">{children}</div>}
    </li>
  );
}

function MacInstall() {
  return (
    <ol className="flex flex-col gap-5">
      <Step n={1} title="Find the current version in the manifest">
        <CodeBlock language="bash">{`curl -s ${MANIFEST_URL} | jq -r '.latest.speakeasyd.version'`}</CodeBlock>
      </Step>
      <Step
        n={2}
        title="Download the daemon + CLI (Apple Silicon shown; use darwin_amd64 for Intel)"
      >
        <CodeBlock language="bash">{`VERSION=${EXAMPLE_VERSION}
BASE=${RELEASES_BASE}/v$VERSION
curl -fSL -o speakeasyd "$BASE/speakeasyd_${"${VERSION}"}_darwin_arm64"
curl -fSL -o speakeasy  "$BASE/speakeasy_${"${VERSION}"}_darwin_arm64"`}</CodeBlock>
      </Step>
      <Step n={3} title="Make them executable and move into your PATH">
        <CodeBlock language="bash">{`chmod +x speakeasyd speakeasy
sudo mv speakeasyd speakeasy /usr/local/bin/`}</CodeBlock>
      </Step>
      <Step n={4} title="Register and start the daemon (LaunchAgent)">
        <CodeBlock language="bash">{`speakeasyd -service install
speakeasyd -service start`}</CodeBlock>
      </Step>
      <Step
        n={5}
        title="Enroll (personal / PoC only — MDM-managed devices are configured by IT, see below)"
      >
        <CodeBlock language="bash">{`speakeasy enroll`}</CodeBlock>
      </Step>
    </ol>
  );
}

function WindowsInstall() {
  return (
    <ol className="flex flex-col gap-5">
      <Step n={1} title="Find the current version in the manifest">
        <CodeBlock language="powershell">{`(Invoke-RestMethod ${MANIFEST_URL}).latest.speakeasyd.version`}</CodeBlock>
      </Step>
      <Step n={2} title="Download the daemon + CLI">
        <CodeBlock language="powershell">{`$VERSION = "${EXAMPLE_VERSION}"
$BASE = "${RELEASES_BASE}/v$VERSION"
Invoke-WebRequest "$BASE/speakeasyd_${"${VERSION}"}_windows_amd64.exe" -OutFile speakeasyd.exe
Invoke-WebRequest "$BASE/speakeasy_${"${VERSION}"}_windows_amd64.exe"  -OutFile speakeasy.exe`}</CodeBlock>
      </Step>
      <Step n={3} title="Register and start the Windows service">
        <CodeBlock language="powershell">{`.\\speakeasyd.exe -service install
.\\speakeasyd.exe -service start`}</CodeBlock>
      </Step>
      <Step
        n={4}
        title="Enroll (personal / PoC only — MDM-managed devices are configured by IT, see below)"
      >
        <CodeBlock language="powershell">{`.\\speakeasy.exe enroll`}</CodeBlock>
      </Step>
    </ol>
  );
}

function LinuxInstall() {
  return (
    <ol className="flex flex-col gap-5">
      <Step n={1} title="Find the current version in the manifest">
        <CodeBlock language="bash">{`curl -s ${MANIFEST_URL} | jq -r '.latest.speakeasyd.version'`}</CodeBlock>
      </Step>
      <Step
        n={2}
        title="Download the daemon + CLI (amd64 shown; use linux_arm64 for ARM)"
      >
        <CodeBlock language="bash">{`VERSION=${EXAMPLE_VERSION}
BASE=${RELEASES_BASE}/v$VERSION
curl -fSL -o speakeasyd "$BASE/speakeasyd_${"${VERSION}"}_linux_amd64"
curl -fSL -o speakeasy  "$BASE/speakeasy_${"${VERSION}"}_linux_amd64"`}</CodeBlock>
      </Step>
      <Step n={3} title="Make them executable and move into your PATH">
        <CodeBlock language="bash">{`chmod +x speakeasyd speakeasy
sudo mv speakeasyd speakeasy /usr/local/bin/`}</CodeBlock>
      </Step>
      <Step n={4} title="Register and start the daemon (systemd)">
        <CodeBlock language="bash">{`speakeasyd -service install
speakeasyd -service start`}</CodeBlock>
      </Step>
      <Step
        n={5}
        title="Enroll (personal / PoC only — MDM-managed devices are configured by IT, see below)"
      >
        <CodeBlock language="bash">{`speakeasy enroll`}</CodeBlock>
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
    required: true,
    notes: "Schema version. Currently 1.",
  },
  {
    field: "email",
    type: "string",
    required: true,
    notes:
      "The user's verified work email. Shown in the agent UI as the enrolled identity.",
  },
  {
    field: "org_token",
    type: "string",
    required: true,
    notes: "Bearer token the agent uses to call Speakeasy. Treat as a secret.",
  },
  {
    field: "org_slug",
    type: "string",
    required: false,
    notes: "Short org identifier (e.g. acme-corp).",
  },
  {
    field: "org_name",
    type: "string",
    required: false,
    notes: "Display name (e.g. Acme Corporation).",
  },
  {
    field: "auto_update",
    type: "string",
    required: false,
    notes: 'One of "disabled" (default), "notify", or "automatic".',
  },
];

const EXAMPLE_MANAGED_JSON = `{
  "v": 1,
  "email": "jane.doe@acme.corp",
  "org_token": "spk_org_REPLACE_ME",
  "org_slug": "acme-corp",
  "org_name": "Acme Corporation",
  "auto_update": "notify"
}`;

export default function DeviceAgent() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["org:read", "org:admin"]} level="page">
          <Page.Section>
            <Page.Section.Title>Device Agent</Page.Section.Title>
            <Page.Section.Description>
              The Speakeasy device agent runs on developer laptops and enforces
              your org's required AI-tool plugins and MCP configuration. Deploy
              it fleet-wide via MDM, or install it individually for testing.
            </Page.Section.Description>
            <Page.Section.Body>
              <Alert variant="info">
                <Icon name="info" className="h-4 w-4" />
                <AlertTitle>
                  Interim install — signed installers in progress
                </AlertTitle>
                <AlertDescription>
                  Signed <code>.pkg</code> / <code>.msi</code> /{" "}
                  <code>.deb</code> installers and one-click MDM binary
                  deployment are still being built. Until they ship, use the
                  manual steps below (the binaries are published but not yet
                  code-signed). The <strong>managed configuration</strong> below
                  is fully supported today.
                </AlertDescription>
              </Alert>
            </Page.Section.Body>
          </Page.Section>

          <Page.Section>
            <Page.Section.Title>Install the agent</Page.Section.Title>
            <Page.Section.Description>
              Download the daemon (<code>speakeasyd</code>) and CLI (
              <code>speakeasy</code>) for your platform, then register the
              daemon as a managed service.
            </Page.Section.Description>
            <Page.Section.Body>
              <Tabs defaultValue="macos">
                <TabsList className="grid w-full max-w-md grid-cols-3">
                  <TabsTrigger value="macos">macOS</TabsTrigger>
                  <TabsTrigger value="windows">Windows</TabsTrigger>
                  <TabsTrigger value="linux">Linux</TabsTrigger>
                </TabsList>
                <TabsContent value="macos" className="pt-4">
                  <MacInstall />
                </TabsContent>
                <TabsContent value="windows" className="pt-4">
                  <WindowsInstall />
                </TabsContent>
                <TabsContent value="linux" className="pt-4">
                  <LinuxInstall />
                </TabsContent>
              </Tabs>
            </Page.Section.Body>
          </Page.Section>

          <Page.Section>
            <Page.Section.Title>MDM configuration</Page.Section.Title>
            <Page.Section.Description>
              For fleet deployment, IT provisions a <code>managed.json</code>{" "}
              file via your MDM (Kandji, Jamf, Intune, …). The agent reads it at
              startup — no per-user enrollment step on the device.
            </Page.Section.Description>
            <Page.Section.Body>
              <div className="flex flex-col gap-6">
                <div>
                  <Type className="mb-2 font-medium">File location</Type>
                  <Type small muted className="mb-3">
                    Deploy the file to the fixed system path for each OS,
                    created with <code>0700</code> directory perms /{" "}
                    <code>0600</code> on the file (or equivalent ACLs on
                    Windows). The agent only reads it — it never writes it.
                  </Type>
                  <div className="overflow-hidden rounded-lg border">
                    <table className="w-full text-sm">
                      <thead className="bg-muted/50 text-muted-foreground">
                        <tr>
                          <th className="px-4 py-2 text-left font-medium">
                            OS
                          </th>
                          <th className="px-4 py-2 text-left font-medium">
                            Path
                          </th>
                          <th className="px-4 py-2 text-left font-medium">
                            Owner
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        {MANAGED_CONFIG_PATHS.map((row) => (
                          <tr key={row.os} className="border-t">
                            <td className="px-4 py-2">{row.os}</td>
                            <td className="px-4 py-2 font-mono text-xs">
                              {row.path}
                            </td>
                            <td className="px-4 py-2">{row.owner}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>

                <div>
                  <Type className="mb-2 font-medium">Schema</Type>
                  <div className="overflow-hidden rounded-lg border">
                    <table className="w-full text-sm">
                      <thead className="bg-muted/50 text-muted-foreground">
                        <tr>
                          <th className="px-4 py-2 text-left font-medium">
                            Field
                          </th>
                          <th className="px-4 py-2 text-left font-medium">
                            Type
                          </th>
                          <th className="px-4 py-2 text-left font-medium">
                            Required
                          </th>
                          <th className="px-4 py-2 text-left font-medium">
                            Notes
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        {MANAGED_CONFIG_FIELDS.map((row) => (
                          <tr key={row.field} className="border-t align-top">
                            <td className="px-4 py-2 font-mono text-xs">
                              {row.field}
                            </td>
                            <td className="px-4 py-2">{row.type}</td>
                            <td className="px-4 py-2">
                              {row.required ? "yes" : "no"}
                            </td>
                            <td className="text-muted-foreground px-4 py-2">
                              {row.notes}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>

                <div>
                  <Type className="mb-2 font-medium">Example managed.json</Type>
                  <CodeBlock language="json">{EXAMPLE_MANAGED_JSON}</CodeBlock>
                </div>

                <Alert variant="warning">
                  <Icon name="triangle-alert" className="h-4 w-4" />
                  <AlertTitle>Where the org token comes from</AlertTitle>
                  <AlertDescription>
                    Self-service <code>org_token</code> issuance is part of the
                    Speakeasy control plane, which is still in progress. For
                    now, contact your Speakeasy representative to obtain a token
                    for your organization.
                  </AlertDescription>
                </Alert>

                <div>
                  <Type className="mb-2 font-medium">Deploying via MDM</Type>
                  <Type small muted>
                    The exact mechanics vary by platform. The common pattern:
                    package <code>managed.json</code> as a custom configuration
                    profile that drops the file at the path above with the right
                    permissions, then scope it to your target device groups.
                    Managed configuration always takes precedence over a user's
                    local enrollment, so an MDM-provisioned device shows as
                    "Provisioned by IT" and ignores any local sign-in.
                  </Type>
                </div>
              </div>
            </Page.Section.Body>
          </Page.Section>

          <Page.Section>
            <Page.Section.Title>
              Coming soon <Badge variant="secondary">In progress</Badge>
            </Page.Section.Title>
            <Page.Section.Body>
              <Type small muted>
                Signed installer packages (<code>.pkg</code> / <code>.msi</code>{" "}
                / <code>.deb</code>), one-click MDM binary deployment, and the
                menu-bar UI download will land here as they ship.
              </Type>
            </Page.Section.Body>
          </Page.Section>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
