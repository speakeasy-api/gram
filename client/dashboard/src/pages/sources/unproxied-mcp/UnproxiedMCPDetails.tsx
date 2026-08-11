import { DetailHero } from "@/components/detail-hero";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import {
  SourceInfoRow,
  SourceInfoTable,
} from "@/components/sources/SourceInfoTable";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Dialog } from "@/components/ui/Dialog";
import { Heading } from "@/components/ui/Heading";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { dateTimeFormatters } from "@/lib/dates";
import {
  formatRemoteMcpDisplay,
  getUnproxiedMcpServerArgs,
} from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { UnproxiedMcpServer } from "@gram/client/models/components/unproxiedmcpserver.js";
import { useGetUnproxiedMcpServer } from "@gram/client/react-query/getUnproxiedMcpServer.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { Server, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { Navigate, useParams } from "react-router";
import { RemoveUnproxiedMcpDialogContent } from "./RemoveUnproxiedMcpDialog";

export default function UnproxiedMCPDetails(): JSX.Element {
  const { sourceSlug } = useParams<{ sourceSlug: string }>();
  const routes = useRoutes();
  const idOrSlug = sourceSlug ?? "";

  const {
    data: server,
    isLoading,
    isError,
  } = useGetUnproxiedMcpServer(getUnproxiedMcpServerArgs(idOrSlug), undefined, {
    enabled: idOrSlug !== "",
  });

  const unproxiedMcpServerId = server?.id ?? "";
  const {
    data: mcpServersResult,
    isLoading: isLoadingLinkedServers,
    isError: isErrorLinkedServers,
  } = useMcpServers({ unproxiedMcpServerId }, undefined, {
    enabled: unproxiedMcpServerId !== "",
  });
  const linkedMcpServers = useMcpServersForUnproxied(
    mcpServersResult?.mcpServers,
    unproxiedMcpServerId,
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
          skipSegments={["unproxiedmcp"]}
        />
      </Page.Header>

      <Page.Body
        fullWidth
        noPadding
        fullHeight
        overflowHidden
        className="gap-0"
      >
        <UnproxiedMcpHero server={server} />

        <div className="mx-auto w-full max-w-[1270px] flex-1 overflow-y-auto px-8 py-6">
          <Stack gap={4} className="max-w-2xl">
            {server && (
              <SourceInfoTable>
                <SourceInfoRow label="URL">
                  <Text small className="break-all">
                    {server.url}
                  </Text>
                </SourceInfoRow>
                {server.description && (
                  <SourceInfoRow label="Description">
                    <Text small>{server.description}</Text>
                  </SourceInfoRow>
                )}
                <SourceInfoRow label="Created">
                  <Text small muted>
                    {dateTimeFormatters.full.format(new Date(server.createdAt))}
                  </Text>
                </SourceInfoRow>
              </SourceInfoTable>
            )}

            <Text muted small>
              This server is listed but never proxied — Speakeasy doesn&apos;t
              fetch its URL or manage OAuth for it. Customers connect to the
              vendor directly.
            </Text>

            <RequireScope scope="mcp:write" level="component">
              {({ disabled }) => (
                <div>
                  <Button
                    variant="secondary"
                    disabled={
                      disabled ||
                      !server ||
                      isLoadingLinkedServers ||
                      isErrorLinkedServers
                    }
                    onClick={() => setIsRemoveOpen(true)}
                  >
                    <Button.LeftIcon>
                      <Trash2 className="text-destructive size-4" />
                    </Button.LeftIcon>
                    <Button.Text>Delete server</Button.Text>
                  </Button>
                  {isErrorLinkedServers && (
                    <Text small className="text-destructive mt-2">
                      Couldn&apos;t check whether other servers depend on this
                      one — refresh the page to try deleting again.
                    </Text>
                  )}
                </div>
              )}
            </RequireScope>
          </Stack>
        </div>
      </Page.Body>

      <Dialog open={isRemoveOpen} onOpenChange={setIsRemoveOpen}>
        <Dialog.Content className="max-w-2xl!">
          {server && (
            <RemoveUnproxiedMcpDialogContent
              unproxiedMcpServerId={server.id}
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
function useMcpServersForUnproxied(
  servers: McpServer[] | undefined,
  unproxiedMcpServerId: string,
) {
  return useMemo(() => {
    if (!servers || !unproxiedMcpServerId) return [];
    return servers.filter(
      (server) => server.unproxiedMcpServerId === unproxiedMcpServerId,
    );
  }, [servers, unproxiedMcpServerId]);
}

function UnproxiedMcpHero({
  server,
}: {
  server: UnproxiedMcpServer | undefined;
}) {
  return (
    <DetailHero>
      <Stack gap={2}>
        <Page.Eyebrow />
        <Stack direction="horizontal" gap={3} align="center">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center bg-amber-500/10 dark:bg-amber-500/20">
            <Server className="h-5 w-5 text-amber-600 dark:text-amber-400" />
          </div>
          <Heading variant="h1" className="break-all normal-case">
            {server ? formatRemoteMcpDisplay(server) : "Unproxied MCP server"}
          </Heading>
          <Badge variant="neutral">
            <Badge.Text>Unproxied MCP · Not proxied</Badge.Text>
          </Badge>
          {server && (
            <CopyButton text={server.url} tooltip="Copy URL" size="xs" />
          )}
        </Stack>
      </Stack>
    </DetailHero>
  );
}
