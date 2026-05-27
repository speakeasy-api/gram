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

function MacInstall() {
  return (
    <ol className="flex flex-col gap-5">
      <Step n={1} title="Find the current version">
        <StepNote>
          The manifest lists the latest published version for each binary.
        </StepNote>
        <CodeBlock language="bash">{`curl -s ${MANIFEST_URL} | jq -r '.latest.speakeasyd.version'`}</CodeBlock>
      </Step>
      <Step n={2} title="Download the daemon + CLI">
        <StepNote>
          Apple Silicon shown — swap <code>darwin_arm64</code> for{" "}
          <code>darwin_amd64</code> on Intel. Replace <code>0.1.0</code> with
          the version from step 1.
        </StepNote>
        <CodeBlock language="bash">{`VERSION=0.1.0
BASE=${RELEASES_BASE}/v$VERSION
curl -fSL -o speakeasyd "$BASE/speakeasyd_\${VERSION}_darwin_arm64"
curl -fSL -o speakeasy  "$BASE/speakeasy_\${VERSION}_darwin_arm64"`}</CodeBlock>
      </Step>
      <Step n={3} title="Make them executable and move into your PATH">
        <CodeBlock language="bash">{`chmod +x speakeasyd speakeasy
sudo mv speakeasyd speakeasy /usr/local/bin/`}</CodeBlock>
      </Step>
      <Step n={4} title="Register and start the background service">
        <StepNote>
          Installs <code>speakeasyd</code> as a LaunchAgent so it runs on login.
        </StepNote>
        <CodeBlock language="bash">{`speakeasyd -service install
speakeasyd -service start`}</CodeBlock>
      </Step>
      <Step n={5} title="Verify it's running">
        <CodeBlock language="bash">{`speakeasy status`}</CodeBlock>
      </Step>
    </ol>
  );
}

function WindowsInstall() {
  return (
    <ol className="flex flex-col gap-5">
      <Step n={1} title="Find the current version">
        <CodeBlock language="powershell">{`(Invoke-RestMethod ${MANIFEST_URL}).latest.speakeasyd.version`}</CodeBlock>
      </Step>
      <Step n={2} title="Download the daemon + CLI">
        <StepNote>
          Replace <code>0.1.0</code> with the version from step 1.
        </StepNote>
        <CodeBlock language="powershell">{`$VERSION = "0.1.0"
$BASE = "${RELEASES_BASE}/v$VERSION"
Invoke-WebRequest "$BASE/speakeasyd_\${VERSION}_windows_amd64.exe" -OutFile speakeasyd.exe
Invoke-WebRequest "$BASE/speakeasy_\${VERSION}_windows_amd64.exe"  -OutFile speakeasy.exe`}</CodeBlock>
      </Step>
      <Step n={3} title="Register and start the Windows service">
        <CodeBlock language="powershell">{`.\\speakeasyd.exe -service install
.\\speakeasyd.exe -service start`}</CodeBlock>
      </Step>
      <Step n={4} title="Verify it's running">
        <CodeBlock language="powershell">{`.\\speakeasy.exe status`}</CodeBlock>
      </Step>
    </ol>
  );
}

function LinuxInstall() {
  return (
    <ol className="flex flex-col gap-5">
      <Step n={1} title="Find the current version">
        <CodeBlock language="bash">{`curl -s ${MANIFEST_URL} | jq -r '.latest.speakeasyd.version'`}</CodeBlock>
      </Step>
      <Step n={2} title="Download the daemon + CLI">
        <StepNote>
          amd64 shown — swap <code>linux_amd64</code> for{" "}
          <code>linux_arm64</code> on ARM. Replace <code>0.1.0</code> with the
          version from step 1.
        </StepNote>
        <CodeBlock language="bash">{`VERSION=0.1.0
BASE=${RELEASES_BASE}/v$VERSION
curl -fSL -o speakeasyd "$BASE/speakeasyd_\${VERSION}_linux_amd64"
curl -fSL -o speakeasy  "$BASE/speakeasy_\${VERSION}_linux_amd64"`}</CodeBlock>
      </Step>
      <Step n={3} title="Make them executable and move into your PATH">
        <CodeBlock language="bash">{`chmod +x speakeasyd speakeasy
sudo mv speakeasyd speakeasy /usr/local/bin/`}</CodeBlock>
      </Step>
      <Step n={4} title="Register and start the background service">
        <StepNote>
          Installs <code>speakeasyd</code> as a systemd service.
        </StepNote>
        <CodeBlock language="bash">{`speakeasyd -service install
speakeasyd -service start`}</CodeBlock>
      </Step>
      <Step n={5} title="Verify it's running">
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
    required: "yes",
    notes:
      "The user's verified work email. Shown in the agent UI as the enrolled identity.",
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

const EXAMPLE_MANAGED_JSON = `{
  "v": 1,
  "email": "jane.doe@acme.corp",
  "org_token": "spk_org_REPLACE_ME",
  "org_slug": "acme-corp",
  "org_name": "Acme Corporation",
  "auto_update": "notify"
}`;

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
              your org's required AI-tool plugins and MCP configuration, then
              reports compliance back to Speakeasy.
            </Page.Section.Description>
            <Page.Section.Body>
              <div className="flex flex-col gap-4">
                <div className="grid gap-4 @2xl/main:grid-cols-2">
                  <div className="rounded-lg border p-4">
                    <div className="mb-1 flex items-center gap-2">
                      <Icon name="building-2" className="h-4 w-4" />
                      <Type className="font-medium">
                        Fleet deployment (recommended)
                      </Type>
                    </div>
                    <Type small muted>
                      IT pushes the agent + a <code>managed.json</code> to every
                      device via MDM. No per-user step; identity is set by IT.
                      See{" "}
                      <a href="#managed-config" className="underline">
                        Managed configuration
                      </a>
                      .
                    </Type>
                  </div>
                  <div className="rounded-lg border p-4">
                    <div className="mb-1 flex items-center gap-2">
                      <Icon name="user" className="h-4 w-4" />
                      <Type className="font-medium">
                        Manual install (personal / PoC)
                      </Type>
                    </div>
                    <Type small muted>
                      Install the binaries yourself and sign in with{" "}
                      <code>speakeasy enroll</code>. Good for testing. Follow{" "}
                      <a href="#install" className="underline">
                        Install the agent
                      </a>
                      .
                    </Type>
                  </div>
                </div>
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
                    code-signed). The managed configuration is fully supported
                    today.
                  </AlertDescription>
                </Alert>
              </div>
            </Page.Section.Body>
          </Page.Section>

          <Page.Section>
            <div id="install" />
            <Page.Section.Title>Install the agent</Page.Section.Title>
            <Page.Section.Description>
              The agent is two binaries: <code>speakeasyd</code>, the background
              daemon that does the enforcement, and <code>speakeasy</code>, the
              CLI you use to check status and enroll. Pick your platform.
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
              <Alert variant="info" className="mt-4">
                <Icon name="info" className="h-4 w-4" />
                <AlertTitle>Enrolling a personal install</AlertTitle>
                <AlertDescription>
                  On a device <em>not</em> managed by MDM, sign in with{" "}
                  <code>speakeasy enroll</code> — it opens a browser, you sign
                  in, and the agent stores your email locally. MDM-managed
                  devices skip this; their identity comes from{" "}
                  <code>managed.json</code>.
                </AlertDescription>
              </Alert>
            </Page.Section.Body>
          </Page.Section>

          <Page.Section>
            <div id="managed-config" />
            <Page.Section.Title>Managed configuration</Page.Section.Title>
            <Page.Section.Description>
              For fleet deployment, IT provisions a <code>managed.json</code>{" "}
              file via your MDM (Kandji, Jamf, Intune, …). The agent reads it at
              startup and applies the identity automatically — no per-user
              enrollment on the device.
            </Page.Section.Description>
            <Page.Section.Body>
              <div className="flex flex-col gap-8">
                <div>
                  <SubHeading>Two config layers</SubHeading>
                  <Type small muted>
                    The agent merges two files per field, with{" "}
                    <code>managed.json</code> (IT-owned) always winning over{" "}
                    <code>local.json</code> (written by a user's{" "}
                    <code>speakeasy enroll</code>). So IT can set{" "}
                    <code>org_token</code> centrally while a user's email comes
                    from either layer. On a fully MDM-managed device,{" "}
                    <code>managed.json</code> supplies everything and the device
                    shows as "Provisioned by IT".
                  </Type>
                </div>

                <div>
                  <SubHeading>File location</SubHeading>
                  <Type small muted className="mb-3">
                    Deploy the file to the fixed system path for each OS. Create
                    the directory <code>0700</code> and the file{" "}
                    <code>0600</code> (or equivalent ACLs on Windows). The file
                    must be{" "}
                    <strong>readable by the user the agent runs as</strong> —
                    the agent runs as the logged-in user, not root, so{" "}
                    <code>0600 root:wheel</code> on macOS won't work; use{" "}
                    <code>0644</code> or a read ACL for the user. The agent only
                    reads this file; it never writes it.
                  </Type>
                  <Table headers={["OS", "Path", "Owner"]}>
                    {MANAGED_CONFIG_PATHS.map((row) => (
                      <tr key={row.os} className="border-t">
                        <td className="px-4 py-2">{row.os}</td>
                        <td className="px-4 py-2 font-mono text-xs">
                          {row.path}
                        </td>
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
                        <td className="px-4 py-2 font-mono text-xs">
                          {row.field}
                        </td>
                        <td className="px-4 py-2">{row.type}</td>
                        <td className="px-4 py-2">{row.required}</td>
                        <td className="text-muted-foreground px-4 py-2">
                          {row.notes}
                        </td>
                      </tr>
                    ))}
                  </Table>
                  <Type small muted className="mt-2">
                    <code>auto_update</code> controls self-update:{" "}
                    <code>"disabled"</code> never checks, <code>"notify"</code>{" "}
                    surfaces available updates without installing, and{" "}
                    <code>"automatic"</code> downloads and installs them. For
                    MDM fleets, <code>"notify"</code> keeps IT in control of
                    what rolls out.
                  </Type>
                </div>

                <div>
                  <SubHeading>Example managed.json</SubHeading>
                  <CodeBlock language="json">{EXAMPLE_MANAGED_JSON}</CodeBlock>
                  <Type small muted className="mt-2">
                    Only <code>v</code>, <code>email</code>, and{" "}
                    <code>org_token</code> are required; the rest are optional.
                  </Type>
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
                  <SubHeading>How the agent applies it</SubHeading>
                  <ul className="text-muted-foreground flex flex-col gap-2 text-sm">
                    <li>
                      <strong className="text-foreground">Startup:</strong> the
                      daemon reads <code>managed.json</code> (and{" "}
                      <code>local.json</code>) when it boots and resolves the
                      merged identity.
                    </li>
                    <li>
                      <strong className="text-foreground">Live updates:</strong>{" "}
                      the daemon watches both files; when MDM pushes a new{" "}
                      <code>managed.json</code> the agent reloads within ~100
                      ms, no restart required.
                    </li>
                    <li>
                      <strong className="text-foreground">Sign out:</strong> a
                      user signing out clears only <code>local.json</code>;{" "}
                      <code>managed.json</code> is untouched, so the device
                      stays enrolled under the managed identity.
                    </li>
                    <li>
                      <strong className="text-foreground">Invalid file:</strong>{" "}
                      a malformed file or a <code>v</code> newer than the agent
                      supports is rejected and surfaced in{" "}
                      <code>speakeasy status</code>; the last good config is not
                      retained.
                    </li>
                  </ul>
                </div>

                <div>
                  <SubHeading>Security</SubHeading>
                  <ul className="text-muted-foreground flex flex-col gap-2 text-sm">
                    <li>
                      <code>org_token</code> is a credential — distribute it the
                      way you'd distribute any API key, and don't commit it or
                      paste it into chat.
                    </li>
                    <li>
                      The agent never writes <code>managed.json</code> and does
                      not log the token or email (PII is redacted at the logging
                      layer).
                    </li>
                  </ul>
                </div>

                <div>
                  <SubHeading>Deploying via MDM</SubHeading>
                  <Type small muted>
                    Mechanics vary by platform. The common pattern: package{" "}
                    <code>managed.json</code> as a custom configuration profile
                    that drops the file at the path above with the right
                    permissions, then scope it to your target device groups. If
                    the agent isn't picking up the file, confirm the path with{" "}
                    <code>speakeasy config path</code>, check that the file is
                    readable by the logged-in user, and validate the JSON.
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
