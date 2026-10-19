import { CodeBlock } from "@/components/code";
import { SpeakeasyMark } from "@/components/mcp/TeamGatewayCard";
import {
  GatewayNotServed,
  OrganizationGatewaySwitch,
  useOrganizationGatewaySetting,
} from "@/components/mcp/organization-gateway-setting";
import { SettingsPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";
import {
  claudeCodeAddCommand,
  codexAddCommand,
  codexLoginCommand,
  cursorInstallLink,
  openCodeConfig,
} from "@/lib/mcp-client-install";
import { ArrowUpRight } from "lucide-react";
import type { ReactNode } from "react";

/**
 * The Team Gateway: one MCP URL that gives each person every server they can
 * reach across the organization's projects. Opened from its card at the head
 * of the MCP Servers list.
 */
export default function TeamGateway(): JSX.Element {
  const organization = useOrganization();
  return (
    <RequireScope scope="org:read" level="page">
      <TeamGatewayContent
        key={organization.id}
        organizationId={organization.id}
        organizationSlug={organization.slug}
      />
    </RequireScope>
  );
}

function TeamGatewayContent({
  organizationId,
  organizationSlug,
}: {
  organizationId: string;
  organizationSlug: string;
}): JSX.Element {
  const isCurrentOrganization = useIsCurrentOrganization(organizationId);
  const { features, gateway, on, setOn, isPending } =
    useOrganizationGatewaySetting({ organizationId, isCurrentOrganization });

  // The stored setting is the source of truth for the switch; the gateway's
  // own answer says the same thing when the setting can't be read.
  const isOn =
    on ??
    (gateway.data
      ? gateway.data.unavailableReason !== "turned_off"
      : undefined);

  return (
    <SettingsPage
      title={
        <span className="inline-flex items-center gap-3">
          <span className="border-border bg-card flex size-9 shrink-0 items-center justify-center border">
            <SpeakeasyMark className="size-6" />
          </span>
          Team Gateway
        </span>
      }
      description={
        <span className="block max-w-2xl">
          A single endpoint through which your team can access all of their
          servers. Each person signs in once with their Speakeasy account and
          gets every MCP server they have access to, across all projects. As
          their access changes, so does what they see.
        </span>
      }
      primaryAction={
        isOn !== undefined && !features.isLoading ? (
          <label className="flex items-center gap-3">
            <Text small muted as="span">
              {isOn ? "Enabled" : "Off"}
            </Text>
            <OrganizationGatewaySwitch
              on={isOn}
              setOn={setOn}
              isPending={isPending}
            />
          </label>
        ) : undefined
      }
    >
      <GatewayBody
        gateway={gateway}
        isOn={isOn}
        serverName={organizationSlug}
      />
    </SettingsPage>
  );
}

function GatewayBody({
  gateway,
  isOn,
  serverName,
}: {
  gateway: ReturnType<typeof useOrganizationGatewaySetting>["gateway"];
  isOn: boolean | undefined;
  serverName: string;
}): JSX.Element {
  if (gateway.isLoading) {
    return (
      <div className="space-y-8">
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-72 w-full" />
      </div>
    );
  }

  if (gateway.error || !gateway.data) {
    return (
      <Alert variant="error">
        <div className="flex flex-1 items-center justify-between gap-3">
          <AlertDescription>
            Couldn&apos;t load the Team Gateway.
          </AlertDescription>
          <Button
            variant="secondary"
            size="sm"
            disabled={gateway.isFetching}
            onClick={() => void gateway.refetch()}
          >
            {gateway.isFetching ? "Retrying…" : "Retry"}
          </Button>
        </div>
      </Alert>
    );
  }

  if (isOn === false) {
    return (
      <Alert variant="warning">
        <AlertDescription>
          Your team can&apos;t reach their servers through this endpoint until
          it is turned back on.
        </AlertDescription>
      </Alert>
    );
  }

  const url = gateway.data.available ? gateway.data.url : undefined;
  if (!url) {
    return (
      <Alert variant="warning">
        <div>
          <AlertTitle>Not available</AlertTitle>
          <GatewayNotServed reason={gateway.data.unavailableReason} />
        </div>
      </Alert>
    );
  }

  return (
    <div className="space-y-10">
      <section>
        <h2 className="text-eyebrow mb-3">Endpoint</h2>
        <div className="border-border bg-card flex items-center gap-3 border px-4 py-3">
          <Text className="min-w-0 flex-1 truncate font-mono text-sm">
            {url}
          </Text>
          <CopyButton text={url} size="sm" tooltip="Copy endpoint" />
        </div>
      </section>

      <section>
        <h2 className="text-eyebrow">Install</h2>
        <Text small muted className="mt-1 mb-4">
          The first time a client connects, it opens your browser to sign in.
        </Text>
        <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
          <InstallCard
            title="Claude Code"
            description="Run this in your terminal, then open /mcp in Claude Code and choose Authenticate."
          >
            <CodeBlock language="bash" copyLabel="command">
              {claudeCodeAddCommand(serverName, url, { multiline: true })}
            </CodeBlock>
          </InstallCard>

          <InstallCard
            title="Claude"
            description="In Claude on the web or desktop, open Settings › Connectors › Add custom connector and paste the endpoint above. On Team and Enterprise plans, an owner adds it for the organization first."
            actions={
              <ExternalButton href="https://claude.ai/settings/connectors">
                Open Claude settings
              </ExternalButton>
            }
          />

          <InstallCard
            title="Cursor"
            description="Adds the gateway to your Cursor MCP settings in one click."
            actions={
              <ExternalButton href={cursorInstallLink(serverName, url)}>
                Add to Cursor
              </ExternalButton>
            }
          />

          <InstallCard
            title="OpenAI Codex"
            description="Run these in your terminal. The second one signs you in."
          >
            <CodeBlock language="bash" copyLabel="commands">
              {`${codexAddCommand(serverName, url, { multiline: true })}\n${codexLoginCommand(serverName)}`}
            </CodeBlock>
          </InstallCard>

          <InstallCard
            title="ChatGPT"
            description="On the web, turn on developer mode under Settings › Apps › Advanced settings, then create an app with the endpoint above as its URL and OAuth as its authentication."
          />

          <InstallCard
            title="OpenCode"
            description="Add this to your opencode.json."
          >
            <CodeBlock language="json" copyLabel="config">
              {openCodeConfig(serverName, url)}
            </CodeBlock>
          </InstallCard>

          <InstallCard
            title="Other clients"
            description="Any client that supports remote MCP servers: add the endpoint above as a Streamable HTTP server and sign in when asked."
          />
        </div>
      </section>
    </div>
  );
}

function InstallCard({
  title,
  description,
  actions,
  children,
}: {
  title: string;
  description: string;
  actions?: ReactNode;
  children?: ReactNode;
}): JSX.Element {
  return (
    <div className="border-border bg-card flex min-w-0 flex-col gap-3 border p-5">
      <div>
        <Text variant="subheading">{title}</Text>
        <Text small muted className="mt-1">
          {description}
        </Text>
      </div>
      {children}
      {actions && (
        <div className="mt-auto flex items-center gap-2">{actions}</div>
      )}
    </div>
  );
}

function ExternalButton({
  href,
  children,
}: {
  href: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <Button variant="secondary" size="sm" asChild>
      <a href={href} target="_blank" rel="noopener noreferrer">
        <Button.Text>{children}</Button.Text>
        <Button.RightIcon>
          <ArrowUpRight />
        </Button.RightIcon>
      </a>
    </Button>
  );
}
