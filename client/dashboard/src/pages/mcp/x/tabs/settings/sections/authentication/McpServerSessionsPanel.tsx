import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { ListUserSessionsQueryParamStatus } from "@gram/client/models/operations/listusersessions.js";
import { useUserSessionsInfinite } from "@gram/client/react-query/userSessions.js";
import { SessionRow } from "@/components/sessions/SessionRow";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { LoadMoreButton } from "@/components/ui/load-more-footer";
import { Button } from "@/components/ui/moonshine";
import { SettingsSection } from "../../SettingsSection";

/**
 * Chrome-free list of an issuer's active sessions. Mounted in the remote
 * server settings tab (wrapped in SettingsSection below) and on the toolset
 * detail page inside that page's own section chrome.
 */
export function UserSessionsList({
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
    <>
      {isPending ? (
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-full" />
          ))}
        </div>
      ) : isError ? (
        <div className="flex items-center justify-between gap-3">
          <Type destructive small>
            Couldn&apos;t load sessions.
          </Type>
          <Button variant="tertiary" size="sm" onClick={() => void refetch()}>
            Retry
          </Button>
        </div>
      ) : sessions.length === 0 ? (
        <Type muted small>
          No active sessions
        </Type>
      ) : (
        <ul className="divide-border divide-y border">
          {sessions.map((s) => (
            <SessionRow
              key={s.id}
              session={s}
              onRevoked={() => void refetch()}
            />
          ))}
        </ul>
      )}
      <LoadMoreButton
        hasMore={hasNextPage}
        isLoading={isFetchingNextPage}
        onLoadMore={() => void fetchNextPage()}
        className="pt-2"
      />
    </>
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
        <SettingsSection.Body>
          {issuerId ? (
            <UserSessionsList issuerId={issuerId} />
          ) : (
            <Type muted small>
              This server isn&apos;t gated by a session issuer.
            </Type>
          )}
        </SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
