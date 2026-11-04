import { mcpServerRouteParam } from "@/lib/sources";
import { Button } from "@/components/ui/Button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { useRoutes } from "@/routes";
import type { AuthClient } from "@gram/client/models/components/authclient.js";
import { useMcpServerAuthentication } from "@gram/client/react-query/mcpServerAuthentication.js";
import { Info, ExternalLink } from "lucide-react";
import type { JSX } from "react";
import type { InventoryServer } from "./useServerInventory";

/** How each kind of auth client signs in upstream, in words. */
const CLIENT_KINDS: Record<string, string> = {
  remote_session: "Each person's own sign-in",
  service_account: "Shared service account",
  static_credential: "Shared credential",
  pass_through: "Caller's own credential",
  ema: "Enterprise managed sign-in",
};

/**
 * How the server signs in to its upstream: one line per way it offers, so a
 * server that takes both a shared token and each person's sign-in says so.
 * Rendered inside the popover, so it asks only once someone opens it.
 */
function ServerSignIn({ server }: { server: InventoryServer }): JSX.Element {
  // A toolset with no MCP server of its own runs on Speakeasy and has no
  // upstream auth configuration to read.
  const hosted = server.kind === "Toolset";
  const auth = useMcpServerAuthentication(
    { mcpServerId: server.id },
    undefined,
    { enabled: !hosted, retry: false },
  );
  const clients: AuthClient[] = (auth.data?.clients ?? []).filter(
    (client) => client.enabled,
  );
  const label =
    clients.length > 1 ? `Sign-in · ${clients.length} options` : "Sign-in";
  const value = ((): JSX.Element | string => {
    if (hosted) return "Hosted by Speakeasy";
    if (auth.isLoading) return "Checking…";
    if (auth.isError || !auth.data) return "Unavailable";
    if (auth.data.status === "no_auth_required") return "No sign-in needed";
    // Needs sign-in and offers no usable way to: something to fix, whoever
    // hosts it.
    if (auth.data.status === "setup_incomplete" && clients.length === 0) {
      return "Not set up";
    }
    if (clients.length === 0) {
      return server.kind === "Hosted" ? "Hosted by Speakeasy" : "Not set up";
    }
    return (
      <ul className="space-y-0.5">
        {clients.map((client) => (
          <li key={client.id} title={client.clientType}>
            {CLIENT_KINDS[client.clientType] ?? client.clientType}
            <span className="text-muted-foreground"> · {client.name}</span>
          </li>
        ))}
      </ul>
    );
  })();
  return (
    <div>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="break-words">{value}</dd>
    </div>
  );
}

export function ServerInfoPopover({
  server,
}: {
  server: InventoryServer;
}): JSX.Element {
  const routes = useRoutes({ projectSlug: server.projectSlug });
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          variant="tertiary"
          size="xs"
          aria-label={`About ${server.name}`}
        >
          <Info className="size-4" aria-hidden="true" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="space-y-3 text-sm">
        <p className="font-medium">{server.name}</p>
        <p className="text-muted-foreground break-words">
          {server.description?.trim() || "No description available."}
        </p>
        <dl className="space-y-2">
          <ServerSignIn server={server} />
          <div>
            <dt className="text-muted-foreground">Slug</dt>
            <dd className="break-all font-mono text-xs">
              {server.slug || "Unavailable"}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">
              Tool calls · last 24 hours
            </dt>
            <dd>{server.recentToolCalls?.toLocaleString() ?? "Unavailable"}</dd>
          </div>
        </dl>
        <a
          href={
            server.kind === "Toolset" && server.toolsetSlug
              ? routes.toolset.href(server.toolsetSlug)
              : routes.mcp.details.overview.href(mcpServerRouteParam(server))
          }
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 underline underline-offset-2"
        >
          Open MCP server <ExternalLink className="size-3" aria-hidden="true" />
        </a>
      </PopoverContent>
    </Popover>
  );
}
