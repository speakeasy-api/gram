import { DetailHero } from "@/components/detail-hero";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import {
  SourceInfoRow,
  SourceInfoTable,
} from "@/components/sources/SourceInfoTable";
import { CopyButton } from "@/components/ui/copy-button";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { dateTimeFormatters } from "@/lib/dates";
import {
  formatRemoteMcpDisplay,
  getPassthroughMcpServerArgs,
} from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { PassthroughMcpServer } from "@gram/client/models/components/passthroughmcpserver.js";
import { useGetPassthroughMcpServer } from "@gram/client/react-query/getPassthroughMcpServer.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { Badge, Button, Dialog, Stack } from "@speakeasy-api/moonshine";
import { Server, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { Navigate, useParams } from "react-router";
import { RemovePassthroughMcpDialogContent } from "./RemovePassthroughMcpDialog";

export default function PassthroughMCPDetails(): JSX.Element {
  const { sourceSlug } = useParams<{ sourceSlug: string }>();
  const routes = useRoutes();
  const idOrSlug = sourceSlug ?? "";

  const {
    data: server,
    isLoading,
    isError,
  } = useGetPassthroughMcpServer(
    getPassthroughMcpServerArgs(idOrSlug),
    undefined,
    { enabled: idOrSlug !== "" },
  );

  const passthroughMcpServerId = server?.id ?? "";
  const { data: mcpServersResult } = useMcpServers(
    { passthroughMcpServerId },
    undefined,
    { enabled: passthroughMcpServerId !== "" },
  );
  const linkedMcpServers = useMcpServersForPassthrough(
    mcpServersResult?.mcpServers,
    passthroughMcpServerId,
  );

  const [isRemoveOpen, setIsRemoveOpen] = useState(false);

  if (isError || (!isLoading && !server)) {
    return <Navigate to={routes.sources.href()} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{
            [idOrSlug]: server ? formatRemoteMcpDisplay(server) : undefined,
          }}
          skipSegments={["passthroughmcp"]}
        />
      </Page.Header>

      <Page.Body
        fullWidth
        noPadding
        fullHeight
        overflowHidden
        className="gap-0"
      >
        <PassthroughMcpHero server={server} />

        <div className="mx-auto w-full max-w-[1270px] flex-1 overflow-y-auto px-8 py-6">
          <Stack gap={4} className="max-w-2xl">
            {server && (
              <SourceInfoTable>
                <SourceInfoRow label="URL">
                  <Type small className="break-all">
                    {server.url}
                  </Type>
                </SourceInfoRow>
                {server.description && (
                  <SourceInfoRow label="Description">
                    <Type small>{server.description}</Type>
                  </SourceInfoRow>
                )}
                <SourceInfoRow label="Created">
                  <Type small muted>
                    {dateTimeFormatters.full.format(new Date(server.createdAt))}
                  </Type>
                </SourceInfoRow>
              </SourceInfoTable>
            )}

            <Type muted small>
              This server is listed but never proxied — Gram doesn&apos;t fetch
              its URL or manage OAuth for it. Customers connect to the vendor
              directly.
            </Type>

            <RequireScope scope="mcp:write" level="component">
              {({ disabled }) => (
                <div>
                  <Button
                    variant="secondary"
                    disabled={disabled || !server}
                    onClick={() => setIsRemoveOpen(true)}
                  >
                    <Button.LeftIcon>
                      <Trash2 className="text-destructive size-4" />
                    </Button.LeftIcon>
                    <Button.Text>Delete server</Button.Text>
                  </Button>
                </div>
              )}
            </RequireScope>
          </Stack>
        </div>
      </Page.Body>

      <Dialog open={isRemoveOpen} onOpenChange={setIsRemoveOpen}>
        <Dialog.Content className="max-w-2xl!">
          {server && (
            <RemovePassthroughMcpDialogContent
              passthroughMcpServerId={server.id}
              url={server.url}
              linkedMcpServers={linkedMcpServers}
              onClose={() => setIsRemoveOpen(false)}
              onSuccess={() => routes.sources.goTo()}
            />
          )}
        </Dialog.Content>
      </Dialog>
    </Page>
  );
}

// mcpServers.list is filtered server-side, but applying the same predicate
// client-side is a defensive guard against a stale or unfiltered cache hit
// invalidating the linkage assumption. Mirrors useMcpServersForRemote in
// RemoteMCPDetails.tsx.
function useMcpServersForPassthrough(
  servers: McpServer[] | undefined,
  passthroughMcpServerId: string,
) {
  return useMemo(() => {
    if (!servers || !passthroughMcpServerId) return [];
    return servers.filter(
      (server) => server.passthroughMcpServerId === passthroughMcpServerId,
    );
  }, [servers, passthroughMcpServerId]);
}

function PassthroughMcpHero({
  server,
}: {
  server: PassthroughMcpServer | undefined;
}) {
  return (
    <DetailHero>
      <Stack gap={2}>
        <Stack direction="horizontal" gap={3} align="center">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-amber-500/10 dark:bg-amber-500/20">
            <Server className="h-5 w-5 text-amber-600 dark:text-amber-400" />
          </div>
          <Heading variant="h1" className="break-all normal-case">
            {server
              ? formatRemoteMcpDisplay(server)
              : "Pass-through MCP server"}
          </Heading>
          <Badge variant="neutral">
            <Badge.Text>Pass-through MCP · Not proxied</Badge.Text>
          </Badge>
          {server && (
            <CopyButton text={server.url} tooltip="Copy URL" size="icon-sm" />
          )}
        </Stack>
      </Stack>
    </DetailHero>
  );
}
