import type { McpServer } from "@gram/client/models/components";
import type { ListUserSessionsQueryParamStatus } from "@gram/client/models/operations";
import { useUserSessionsInfinite } from "@gram/client/react-query";
import { SessionRow } from "@/components/sessions/SessionRow";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { SettingsSection } from "../../SettingsSection";

function McpServerSessionsPanelInner({
  issuerId,
}: {
  issuerId: string;
}): JSX.Element {
  const {
    data,
    isPending,
    isError,
    hasNextPage,
    fetchNextPage,
    isFetchingNextPage,
    refetch,
  } = useUserSessionsInfinite({
    userSessionIssuerId: issuerId,
    status: "active" as ListUserSessionsQueryParamStatus,
  });
  const sessions = data?.pages.flatMap((p) => p.result.items) ?? [];

  return (
    <SettingsSection.Body>
      {isPending ? (
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-full" />
          ))}
        </div>
      ) : isError ? (
        <div className="flex items-center justify-between gap-3">
          <p className="text-destructive text-sm">
            Couldn&apos;t load sessions.
          </p>
          <Button variant="ghost" size="sm" onClick={() => void refetch()}>
            Retry
          </Button>
        </div>
      ) : sessions.length === 0 ? (
        <p className="text-muted-foreground text-sm">No active sessions</p>
      ) : (
        <ul className="divide-border divide-y rounded-md border">
          {sessions.map((s) => (
            <SessionRow
              key={s.id}
              session={s}
              onRevoked={() => void refetch()}
            />
          ))}
        </ul>
      )}
      {hasNextPage && (
        <div className="flex justify-center pt-2">
          <Button
            variant="ghost"
            size="sm"
            disabled={isFetchingNextPage}
            onClick={() => void fetchNextPage()}
          >
            {isFetchingNextPage ? "Loading…" : "Load more"}
          </Button>
        </div>
      )}
    </SettingsSection.Body>
  );
}

export function McpServerSessionsPanel({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element {
  const issuerId = mcpServer.userSessionIssuerId;

  return (
    <SettingsSection id="user-sessions">
      <SettingsSection.Header>
        <SettingsSection.Title>User sessions</SettingsSection.Title>
        <SettingsSection.Description>
          Active sessions clients hold into this server, established via OAuth.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        {issuerId ? (
          <McpServerSessionsPanelInner issuerId={issuerId} />
        ) : (
          <SettingsSection.Body>
            <p className="text-muted-foreground text-sm">
              This server isn&apos;t gated by a session issuer.
            </p>
          </SettingsSection.Body>
        )}
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
