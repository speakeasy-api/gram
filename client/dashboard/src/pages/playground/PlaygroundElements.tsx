import { useToolset } from "@/hooks/toolTypes";
import { useMissingRequiredEnvVars } from "@/hooks/useMissingEnvironmentVariables";
import { useInternalMcpUrl } from "@/hooks/useToolsetUrl";
import type { Toolset } from "@/lib/toolTypes";
import { useRoutes } from "@/routes";
import { useGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";
import { useListEnvironments } from "@gram/client/react-query/listEnvironments.js";
import { AlertCircle, PlugZap, ShieldAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import { usePlaygroundIssuerConnection } from "./usePlaygroundIssuerConnection";
import { PlaygroundChat } from "./PlaygroundChat";

interface PlaygroundElementsProps {
  toolsetSlug: string | null;
  environmentSlug: string | null;
  model: string;
  /** Additional action buttons to render alongside the share button */
  additionalActions?: React.ReactNode;
  /** Slug of the playground environment for user-provided variables */
  playgroundEnvironmentSlug?: string;
}

/**
 * The toolset-backed playground variant: resolves a toolset to its Gram-hosted
 * `/mcp/<slug>` URL, mints an issuer-gated gateway token, surfaces
 * missing-auth / login notices, then renders the shared {@link PlaygroundChat}.
 */
export function PlaygroundElements({
  toolsetSlug,
  environmentSlug,
  model,
  additionalActions,
  playgroundEnvironmentSlug,
}: PlaygroundElementsProps): JSX.Element {
  // Get toolset data to construct MCP URL
  const { data: toolset } = useToolset(toolsetSlug ?? undefined);

  // Always use the platform domain for the playground to avoid CSP issues
  const mcpUrl = useInternalMcpUrl(toolset);

  // Get environments and MCP metadata for auth status check
  const { data: environmentsData } = useListEnvironments();
  const environments = environmentsData?.environments ?? [];
  const { data: mcpMetadataData } = useGetMcpMetadata(
    { toolsetSlug: toolsetSlug ?? "" },
    undefined,
    { throwOnError: false, retry: false, enabled: !!toolsetSlug },
  );
  const mcpMetadata = mcpMetadataData?.metadata;
  const defaultEnvironmentSlug =
    environments.find((env) => env.id === mcpMetadata?.defaultEnvironmentId)
      ?.slug ?? "default";

  // ToolsetEntry from useListToolsets is structurally compatible with Toolset
  // for the fields useMissingRequiredEnvVars accesses (same pattern as Playground.tsx)
  //
  // Intentionally do NOT pass playgroundEnvironmentSlug here. The playground
  // environment only stores user-provided entries, so system variables would
  // always appear missing if we pointed the hook at it. User-provided vars
  // are already treated as always-configured by useMissingRequiredEnvVars
  // regardless of the environment, so using the default env here is correct
  // for both kinds of variables.
  const missingAuthCount = useMissingRequiredEnvVars(
    toolset as Toolset | undefined,
    environments,
    environmentSlug ?? defaultEnvironmentSlug,
    mcpMetadata,
  );

  // Issuer-gated toolsets mint a user-session JWT and link upstream sessions via
  // the first-party connect flow. The shared hook mints the token (sent as the
  // chat's `Authorization: Bearer`) and probes the endpoint so we can block the
  // chat until the dashboard user has connected. React Query dedupes the mint
  // and probe with the auth panel (PlaygroundAuth), so this costs no extra calls.
  const issuerConnection = usePlaygroundIssuerConnection(
    toolset as Toolset | undefined,
  );

  // The minted user-session JWT is what the elements MCP client sends as
  // `Authorization: Bearer` on /mcp/{slug} requests, so the runtime gateway
  // resolves the dashboard user's stored upstream credentials via the same path
  // a real MCP client would after an OAuth dance — no special-casing in
  // ApplyIssuerGate. Minting persists a user_sessions row server-side, so the
  // shared hook only mints after the user explicitly clicks Connect (see
  // ConnectRequiredNotice below) — opening the playground alone must not
  // establish a session. The consent is keyed by toolset id and persisted, so
  // a toolset the user already connected reconnects without another click.
  const gatewayToken = issuerConnection.accessToken;

  // Don't render until we have a valid MCP URL
  if (!mcpUrl || !toolsetSlug) {
    return (
      <div className="flex h-full items-center justify-center">
        <Type muted>Select an MCP server to start chatting</Type>
      </div>
    );
  }

  // Issuer-gated toolsets need an explicit Connect before the playground mints
  // a user-session token (see the gatewayToken comment above). Checked ahead of
  // `needsAuth`: until the user consents nothing is minted or probed.
  if (issuerConnection.needsExplicitConnect) {
    return (
      <ConnectRequiredNotice
        serverName={toolset?.name ?? "this MCP server"}
        onConnect={issuerConnection.requestConnect}
      />
    );
  }

  // Block the chat when an issuer-gated toolset needs the dashboard user to link
  // their upstream session: the probe 401'd (`needsAuth`), so tool calls would
  // fail until they Connect in the auth panel. The connect button lives in
  // PlaygroundAuth (the sidebar), which shares this hook's probe state.
  if (issuerConnection.isIssuerGated && issuerConnection.needsAuth) {
    return <LoginRequiredNotice providerName={toolset?.name ?? "provider"} />;
  }

  return (
    <PlaygroundChat
      mcpUrl={mcpUrl}
      gatewayToken={gatewayToken}
      model={model}
      environmentSlug={environmentSlug}
      playgroundEnvironmentSlug={playgroundEnvironmentSlug}
      toolset={toolset}
      additionalActions={additionalActions}
      banner={
        missingAuthCount > 0 ? (
          <AuthWarningBanner
            missingCount={missingAuthCount}
            toolsetSlug={toolsetSlug}
          />
        ) : undefined
      }
    />
  );
}

function AuthWarningBanner({
  missingCount,
  toolsetSlug,
}: {
  missingCount: number;
  toolsetSlug: string;
}) {
  const routes = useRoutes();

  return (
    <div className="bg-warning/15 border-warning/30 text-warning-foreground flex items-center gap-2 border-b px-4 py-2.5 text-sm font-medium">
      <AlertCircle className="size-4 shrink-0" />
      <span>
        {missingCount} authentication{" "}
        {missingCount === 1 ? "variable" : "variables"} not configured.{" "}
        <routes.mcp.details.Link
          params={[toolsetSlug]}
          hash="authentication"
          className="hover:text-foreground font-medium underline"
        >
          Configure now
        </routes.mcp.details.Link>
      </span>
    </div>
  );
}

/**
 * The explicit-consent gate for issuer-gated toolsets: connecting mints a
 * user-session token, which establishes a session on the server, so we wait
 * for a deliberate click instead of minting on page load.
 */
function ConnectRequiredNotice({
  serverName,
  onConnect,
}: {
  serverName: string;
  onConnect: () => void;
}) {
  return (
    <div className="flex h-full items-center justify-center">
      <div className="flex max-w-md flex-col items-center gap-3 px-4 text-center">
        <div className="bg-muted rounded-full p-3">
          <PlugZap className="text-muted-foreground size-6" />
        </div>
        <Type className="font-medium">Connect to start chatting</Type>
        <Type muted className="text-sm">
          Connecting to{" "}
          <span className="text-foreground font-medium">{serverName}</span>{" "}
          establishes a user session for your account so the playground can call
          its tools on your behalf.
        </Type>
        <Button onClick={onConnect}>Connect</Button>
      </div>
    </div>
  );
}

function LoginRequiredNotice({ providerName }: { providerName: string }) {
  return (
    <div className="flex h-full items-center justify-center">
      <div className="flex max-w-md flex-col items-center gap-3 px-4 text-center">
        <div className="bg-warning/15 rounded-full p-3">
          <ShieldAlert className="text-warning size-6" />
        </div>
        <Type className="font-medium">Login Required</Type>
        <Type muted className="text-sm">
          This MCP server requires you to connect your{" "}
          <span className="text-foreground font-medium">{providerName}</span>{" "}
          account. Use the{" "}
          <span className="text-foreground font-medium">Connect</span> button in
          the Authentication section of the sidebar to sign in.
        </Type>
      </div>
    </div>
  );
}
