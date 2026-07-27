import { CopyButton } from "@/components/ui/copy-button";
import { Card } from "@/components/ui/card";
import { Type } from "@/components/ui/type";
import { getServerURL } from "@/lib/utils";
import type { PulseMCPServer } from "@/pages/catalog/hooks";
import { ExternalLink } from "lucide-react";
import type { ReactNode } from "react";

const GOOGLE_WORKSPACE_PROVIDER = "google-workspace";

export function GoogleWorkspaceSetupGuide({
  server,
}: {
  server: PulseMCPServer;
}): JSX.Element | null {
  const setup = server.meta?.["com.getgram/catalog"];
  if (setup?.provider !== GOOGLE_WORKSPACE_PROVIDER) return null;

  const requiredServices = [
    ...(setup.requiredApis ?? []),
    ...(setup.requiredMcpServices ?? []),
  ];
  const enableCommand = `gcloud services enable ${requiredServices.join(" ")} --project=<PROJECT_ID>`;
  const redirectURI = `${getServerURL()}/mcp/remote_login_callback`;

  return (
    <Card>
      <Card.Header>
        <Card.Title>Google Workspace setup</Card.Title>
      </Card.Header>
      <Card.Content className="space-y-5">
        <SetupStep number={1} title="Enable the Google Cloud services">
          <Type small muted>
            Enable the product API and its MCP service in the Google Cloud
            project that will own the OAuth client.
          </Type>
          <CopyableCode value={enableCommand} />
        </SetupStep>

        <SetupStep number={2} title="Configure OAuth consent scopes">
          <Type small muted>
            Add these scopes in Google Auth Platform → Data Access:
          </Type>
          <ul className="space-y-1">
            {(setup.requiredScopes ?? []).map((scope) => (
              <li key={scope}>
                <code className="text-xs break-all">{scope}</code>
              </li>
            ))}
          </ul>
        </SetupStep>

        <SetupStep number={3} title="Create a customer-owned OAuth client">
          <Type small muted>
            In Google Auth Platform, create a Web application OAuth client. Your
            organization owns the client ID and secret; Gram does not create or
            own them. Register this authorized redirect URI:
          </Type>
          <CopyableCode value={redirectURI} />
        </SetupStep>

        <SetupStep number={4} title="Install and authenticate">
          <Type small muted>
            Add this server, then open its Settings → Authentication section.
            Start with the discovered Google configuration, choose Manual, paste
            the customer-owned client ID and secret, and authenticate.
          </Type>
        </SetupStep>

        <div className="border-t pt-5">
          <Type className="mb-1 font-medium">
            Migrating from the community Workspace connector
          </Type>
          <Type small muted>
            {setup.migrationFromCommunityMcp} Add both official Drive and Docs
            servers to the same assistant when a workflow needs file creation
            and rich document editing. Gram namespaces tools by server, so their
            tools remain distinct. Read the finished document with{" "}
            <code>read_doc</code> before removing the community connector.
          </Type>
        </div>

        {setup.setupDocumentationUrl && (
          <a
            href={setup.setupDocumentationUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="text-primary inline-flex items-center gap-1 text-sm hover:underline"
          >
            Google&apos;s complete setup guide
            <ExternalLink className="size-3.5" />
          </a>
        )}
      </Card.Content>
    </Card>
  );
}

function SetupStep({
  number,
  title,
  children,
}: {
  number: number;
  title: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="space-y-2">
      <Type className="font-medium">
        {number}. {title}
      </Type>
      {children}
    </div>
  );
}

function CopyableCode({ value }: { value: string }): JSX.Element {
  return (
    <div className="bg-muted/50 flex items-center justify-between gap-3 rounded-lg p-3">
      <code className="text-xs break-all">{value}</code>
      <CopyButton size="inline" text={value} tooltip="Copy" />
    </div>
  );
}
