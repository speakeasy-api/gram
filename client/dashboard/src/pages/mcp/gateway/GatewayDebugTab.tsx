import { ConnectionsList } from "@/components/connections/ConnectionsList";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useSessionAgents } from "@/hooks/useSessionAgents";
import { useUserSessionsInfinite } from "@gram/client/react-query/userSessions.js";
import { keepPreviousData } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router";

export function GatewayDebugTab({
  gatewayId,
}: {
  gatewayId: string;
}): JSX.Element {
  const project = useProject();
  const { hasScope } = useRBAC();
  const canRevoke = hasScope("project:write", project.id);
  const [searchParams, setSearchParams] = useSearchParams();
  const search = searchParams.get("q") ?? "";
  const setSearch = useCallback(
    (value: string) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (value) next.set("q", value);
          else next.delete("q");
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const {
    data,
    isPending,
    isError,
    hasNextPage,
    fetchNextPage,
    isFetchingNextPage,
    isFetching,
    isPlaceholderData,
    isFetchNextPageError,
    isRefetchError,
    refetch,
  } = useUserSessionsInfinite(
    {
      gramProject: project.slug,
      metaMcpServerId: gatewayId,
      search: search.trim() || undefined,
    },
    undefined,
    { placeholderData: keepPreviousData },
  );
  const unresolved = useMemo(
    () => data?.pages.flatMap((page) => page.result.items) ?? [],
    [data],
  );
  const sessions = useSessionAgents(unresolved);

  let body: JSX.Element;
  if (isPending) {
    body = (
      <div className="space-y-2">
        {Array.from({ length: 6 }).map((_, i) => (
          <Skeleton key={i} className="h-20 w-full" />
        ))}
      </div>
    );
  } else if (isError && sessions.length === 0) {
    body = (
      <div className="flex items-center justify-between gap-3">
        <p className="text-destructive text-sm">
          Couldn&apos;t load connections.
        </p>
        <Button variant="tertiary" size="sm" onClick={() => void refetch()}>
          Retry
        </Button>
      </div>
    );
  } else if (sessions.length === 0) {
    body = (
      <p className="text-muted-foreground text-sm">
        {search.trim()
          ? "No connections match your search"
          : "No connections have been recorded for this gateway yet"}
      </p>
    );
  } else {
    body = (
      <ConnectionsList
        sessions={sessions}
        grouping="subject"
        canRevoke={canRevoke}
        onRevoked={() => void refetch()}
        killswitchContext={{ capabilityKey: "mcp_tool_calls" }}
        project={project}
      />
    );
  }

  return (
    <Stack gap={4}>
      <Text small muted>
        Sessions issued for this gateway. Sessions without a recorded origin are
        not shown.
      </Text>
      <Page.Toolbar>
        <Page.Toolbar.Search
          value={search}
          onChange={setSearch}
          debounceMs={250}
          placeholder="Search connections"
        />
        <Page.Toolbar.Refresh
          onRefresh={() => void refetch()}
          isRefreshing={isFetching}
        />
      </Page.Toolbar>
      {sessions.length > 0 && (isRefetchError || isFetchNextPageError) && (
        <div className="flex items-center justify-between gap-3">
          <p className="text-destructive text-sm">
            {isFetchNextPageError
              ? "Couldn’t load more connections."
              : "Couldn’t refresh connections."}
          </p>
          <Button
            variant="tertiary"
            size="sm"
            onClick={() =>
              void (isFetchNextPageError ? fetchNextPage() : refetch())
            }
          >
            Retry
          </Button>
        </div>
      )}
      {body}
      {hasNextPage && !isPlaceholderData && (
        <div className="flex justify-center">
          <Button
            variant="tertiary"
            size="sm"
            disabled={isFetching}
            onClick={() => void fetchNextPage()}
          >
            {isFetchingNextPage ? "Loading…" : "Load more"}
          </Button>
        </div>
      )}
    </Stack>
  );
}
