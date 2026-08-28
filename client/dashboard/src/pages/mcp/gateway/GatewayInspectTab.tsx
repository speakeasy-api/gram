import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { CodeSnippet } from "@/components/ui/CodeSnippet";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useMcpConnectConsent } from "@/hooks/useMcpConnectConsent";
import { useUserSessionToken } from "@/hooks/useUserSessionToken";
import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import { mcpConnectionUrl } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { useMemo, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { gatewayTabHref } from "./GatewayDetailsRouting";
import { useGatewayMemberRows } from "./useGatewayMemberRows";
import {
  useGatewayDescribeServer,
  useGatewayInspection,
  type GatewayInspection,
} from "./useGatewayInspection";

/** Renders a tool's argument names, which is what the mock's tool list shows. */
function argumentNames(inputSchema: unknown): string {
  const properties =
    typeof inputSchema === "object" &&
    inputSchema !== null &&
    "properties" in inputSchema
      ? (inputSchema as { properties?: Record<string, unknown> }).properties
      : undefined;
  const names = Object.keys(properties ?? {});
  return names.length > 0 ? names.join(", ") : "(no arguments)";
}

export function GatewayInspectTab({
  metaMcpServer,
  endpoints,
  isLoadingEndpoints,
}: {
  metaMcpServer: MetaMcpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
}): JSX.Element {
  const routes = useRoutes();
  const { mcpUrl, loading } = useResolvedMcpServerUrl(
    endpoints,
    isLoadingEndpoints,
  );
  const connectUrl = useMemo(() => mcpConnectionUrl(mcpUrl), [mcpUrl]);

  // Connect as the signed-in user rather than anonymously, so the tab shows
  // the members this operator can actually reach. The mint RPC requires the
  // same mcp:connect the runtime enforces, so this grants nothing extra —
  // but minting persists a user-session row, so it only happens after an
  // explicit Connect click, persisted per gateway like the other MCP surfaces.
  const isIssuerGated = !!metaMcpServer.userSessionIssuerId;
  const { connectRequested, requestConnect } = useMcpConnectConsent(
    isIssuerGated ? metaMcpServer.id : undefined,
  );
  const needsExplicitConnect = isIssuerGated && !connectRequested;
  const { accessToken, isLoading: isMintingToken } = useUserSessionToken({
    target: { kind: "metaMcpServer", id: metaMcpServer.id },
    userSessionIssuerId: metaMcpServer.userSessionIssuerId,
    enabled: connectRequested,
  });
  const headers = accessToken
    ? { Authorization: `Bearer ${accessToken}` }
    : undefined;

  const { data, isLoading, isError, needsAuth, error, refetch } =
    useGatewayInspection(connectUrl, {
      headers,
      enabled:
        !loading && !!connectUrl && !isMintingToken && !needsExplicitConnect,
    });
  // Configured membership, to explain a shortfall against what the endpoint
  // actually serves this connection.
  const { rows } = useGatewayMemberRows(metaMcpServer.id);

  return (
    <Page.Section>
      <Page.Section.Title>Inspect</Page.Section.Title>
      <Page.Section.Description>
        Exactly what a client receives from this endpoint, read live over MCP.
      </Page.Section.Description>
      <Page.Section.CTA>
        <Button
          variant="secondary"
          size="sm"
          disabled={!data}
          onClick={() => {
            if (!data) return;
            void navigator.clipboard.writeText(JSON.stringify(data, null, 2));
            toast.success("Copied as JSON");
          }}
        >
          <Button.Text>Copy as JSON</Button.Text>
        </Button>
      </Page.Section.CTA>
      <Page.Section.Body>
        <InspectBody
          data={data}
          isLoading={isLoading || loading || isMintingToken}
          isError={isError}
          needsAuth={needsAuth}
          needsExplicitConnect={needsExplicitConnect}
          onConnect={requestConnect}
          gatewayName={metaMcpServer.name}
          error={error}
          hasUrl={!!connectUrl}
          configuredMembers={rows.length}
          connectUrl={connectUrl}
          headers={headers}
          onRetry={refetch}
          settingsHref={gatewayTabHref(routes, metaMcpServer.id, "settings")}
        />
      </Page.Section.Body>
    </Page.Section>
  );
}

function InspectBody({
  data,
  isLoading,
  isError,
  needsAuth,
  needsExplicitConnect,
  onConnect,
  gatewayName,
  error,
  hasUrl,
  configuredMembers,
  connectUrl,
  headers,
  onRetry,
  settingsHref,
}: {
  data: GatewayInspection | undefined;
  isLoading: boolean;
  isError: boolean;
  needsAuth: boolean;
  needsExplicitConnect: boolean;
  onConnect: () => void;
  gatewayName: string;
  error: Error | null;
  hasUrl: boolean;
  configuredMembers: number;
  connectUrl: string | undefined;
  headers: Record<string, string> | undefined;
  onRetry: () => void;
  settingsHref: string;
}): JSX.Element {
  if (!hasUrl && !isLoading) {
    return (
      <Text muted small>
        This gateway has no address yet. Add one in{" "}
        <Link to={settingsHref}>Settings</Link> so clients — and this tab — can
        connect.
      </Text>
    );
  }

  // Issuer-gated gateways only mint the user-session token this tab connects
  // with after an explicit Connect click — minting persists a session row, so
  // opening the tab alone must not establish one.
  if (needsExplicitConnect) {
    return (
      <div className="flex flex-col items-start gap-2">
        <Text muted small>
          Connecting to{" "}
          <span className="text-foreground font-medium">{gatewayName}</span>{" "}
          establishes a user session for your account so this tab can read the
          gateway&apos;s MCP surface as you.
        </Text>
        <Button variant="secondary" size="sm" onClick={onConnect}>
          <Button.Text>Connect</Button.Text>
        </Button>
      </div>
    );
  }

  if (isLoading) {
    return <Skeleton className="h-64 w-full" />;
  }

  if (needsAuth) {
    return (
      <Text muted small>
        This gateway rejected the dashboard&apos;s session. Check its issuer in{" "}
        <Link to={settingsHref}>Settings</Link>.
      </Text>
    );
  }

  if (isError || !data) {
    return (
      <div className="flex flex-col items-start gap-2">
        <Text muted small>
          {error?.message ?? "Couldn't read this gateway's MCP surface."}
        </Text>
        <Button variant="secondary" size="sm" onClick={onRetry}>
          <Button.Text>Try again</Button.Text>
        </Button>
      </div>
    );
  }

  // The tool surface reads as a signature list: one line per tool with its
  // argument names, the way the wire contract is easiest to scan.
  const nameWidth =
    Math.max(...data.tools.map((tool) => tool.name.length), 0) + 2;
  const memberCount = data.servers?.length ?? 0;
  const toolSignatures = [
    `// ${data.tools.length} tools fronting ${memberCount} member ${
      memberCount === 1 ? "server" : "servers"
    }`,
    ...data.tools.map(
      (tool) =>
        `${tool.name.padEnd(nameWidth)}${argumentNames(tool.inputSchema)}`,
    ),
  ].join("\n");

  return (
    <div className="grid grid-cols-1 items-start gap-6 xl:grid-cols-2">
      {/* Left column: the tool surface, then one level down into a member. */}
      <div className="flex flex-col gap-6">
        <InspectCard title="tools/list" meta={`${data.tools.length} tools`}>
          <CodeSnippet language="js" code={toolSignatures} fontSize="small" />
        </InspectCard>

        {(data.servers?.length ?? 0) > 0 && (
          <DescribeServerCard
            servers={data.servers ?? []}
            connectUrl={connectUrl}
            headers={headers}
          />
        )}
      </div>

      {/* Right column: what the agent is told, and the state it can see. */}
      <div className="flex flex-col gap-6">
        <InspectCard title="server instructions" meta="sent on connect">
          {data.instructions ? (
            // Prose, not code: a plain pre keeps the paragraph breaks that a
            // syntax highlighter collapses.
            <pre className="font-mono text-xs leading-relaxed break-words whitespace-pre-wrap">
              {data.instructions}
            </pre>
          ) : (
            <Text muted small>
              This gateway sends no instructions.
            </Text>
          )}
        </InspectCard>

        <InspectCard title="list_servers" meta="bundle state">
          {data.servers === undefined ? (
            <Text muted small>
              The gateway couldn&apos;t report its member state.
            </Text>
          ) : (
            <div className="flex flex-col gap-2">
              <CodeSnippet
                language="json"
                code={JSON.stringify({ servers: data.servers }, null, 2)}
                fontSize="small"
              />
              {data.servers.length < configuredMembers && (
                <Text muted className="text-xs">
                  {`${configuredMembers - data.servers.length} of ${configuredMembers} configured members aren't served to you. Private members need mcp:connect, and members with no gateway dispatch path are excluded.`}
                </Text>
              )}
            </div>
          )}
        </InspectCard>
      </div>
    </div>
  );
}

// One member's catalog, with a picker so every member is reachable rather
// than only the first. Proxied members answer with the runtime's own "not yet
// available" error, which is what a client sees too.
function DescribeServerCard({
  servers,
  connectUrl,
  headers,
}: {
  servers: { slug: string; name?: string }[];
  connectUrl: string | undefined;
  headers: Record<string, string> | undefined;
}): JSX.Element {
  const [selected, setSelected] = useState<string | undefined>();
  const serverSlug =
    selected && servers.some((server) => server.slug === selected)
      ? selected
      : servers[0]?.slug;
  const { data, isLoading } = useGatewayDescribeServer(connectUrl, serverSlug, {
    headers,
  });

  return (
    <InspectCard
      title="describe_server"
      action={
        servers.length > 1 ? (
          <Select value={serverSlug} onValueChange={setSelected}>
            <SelectTrigger className="h-7 w-[190px] font-mono text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {servers.map((server) => (
                <SelectItem
                  key={server.slug}
                  value={server.slug}
                  className="font-mono text-xs"
                >
                  {server.slug}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <Badge variant="neutral">
            <Badge.Text>{`server: "${serverSlug ?? ""}"`}</Badge.Text>
          </Badge>
        )
      }
    >
      {isLoading || !data ? (
        <Skeleton className="h-40 w-full" />
      ) : data.error ? (
        // An error is prose, not JSON: wrap it rather than letting a code
        // block clip it.
        <Text
          muted
          className="font-mono text-xs break-words whitespace-pre-wrap"
        >
          {data.error}
        </Text>
      ) : (
        <CodeSnippet
          language="json"
          code={JSON.stringify(data.result ?? null, null, 2)}
          fontSize="small"
        />
      )}
    </InspectCard>
  );
}

function InspectCard({
  title,
  meta,
  action,
  children,
}: {
  title: string;
  meta?: string;
  /** Replaces the meta badge, e.g. with a picker. */
  action?: React.ReactNode;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="border-border flex flex-col border">
      <div className="border-border flex min-h-[42px] items-center justify-between gap-2 border-b px-4 py-2">
        <Text className="font-mono text-xs tracking-wide uppercase">
          {title}
        </Text>
        {action ?? (
          <Badge variant="neutral">
            <Badge.Text>{meta}</Badge.Text>
          </Badge>
        )}
      </div>
      <div className="border-primary/60 bg-muted/20 border-l-2 px-4 py-3">
        {children}
      </div>
    </div>
  );
}
