import { Combobox } from "@/components/ui/Combobox";
import { CommandItem } from "@/components/ui/Command";
import { FieldError } from "@/components/ui/Field";
import { Label } from "@/components/ui/Label";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useDebouncedValue } from "@/hooks/useDebouncedValue";
import { remoteSessionScopeTier } from "@/lib/sources";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { useRemoteSessionIssuersInfinite } from "@gram/client/react-query/remoteSessionIssuers.js";
import { useMemo, useState } from "react";

const ISSUER_TIER_LABEL = {
  project: "This project",
  organization: "Organization",
  platform: "Platform catalog",
} as const satisfies Record<ReturnType<typeof remoteSessionScopeTier>, string>;

function issuerOptionLabel(issuer: RemoteSessionIssuer): string {
  return `${issuer.name?.trim() || issuer.slug} — ${issuer.issuer}`;
}

// Searched and paged on the server: the listing spans the project, its
// organization and the whole platform catalog, which is too large to load
// in full for a dropdown.
export function AuthorizationServerSelect({
  excludedIssuerIds,
  selectedIssuerId,
  selectedIssuer,
  pickFailed,
  onChange,
}: {
  excludedIssuerIds: ReadonlySet<string>;
  selectedIssuerId: string;
  selectedIssuer: RemoteSessionIssuer | undefined;
  pickFailed: boolean;
  onChange: (id: string) => void;
}): JSX.Element {
  const [search, setSearch] = useState("");
  const debouncedSearch = useDebouncedValue(search.trim(), 250);
  const {
    data,
    isFetching,
    isError,
    hasNextPage,
    isFetchingNextPage,
    fetchNextPage,
  } = useRemoteSessionIssuersInfinite(
    { search: debouncedSearch || undefined },
    undefined,
    { throwOnError: false },
  );
  const issuerOptions = useMemo(
    () =>
      (data?.pages.flatMap((page) => page.result.items) ?? [])
        .filter((issuer) => !excludedIssuerIds.has(issuer.id))
        .map((issuer) => ({
          value: issuer.id,
          label: issuerOptionLabel(issuer),
          // Tiers can hold records with the same name and URL; the tier is
          // what tells them apart.
          description: ISSUER_TIER_LABEL[remoteSessionScopeTier(issuer)],
        })),
    [data, excludedIssuerIds],
  );

  let emptyMessage = "No authorization servers match.";
  if (isError) emptyMessage = "Failed to load authorization servers.";
  else if (isFetching) emptyMessage = "Searching…";

  const loadMore = hasNextPage ? (
    <CommandItem
      value="__load_more__"
      disabled={isFetchingNextPage}
      onSelect={() => void fetchNextPage()}
      className="text-muted-foreground cursor-pointer justify-center"
    >
      {isFetchingNextPage ? "Loading…" : "Load more"}
    </CommandItem>
  ) : null;

  return (
    <Stack gap={2}>
      <Label className="text-muted-foreground text-xs">
        Authorization Server
      </Label>
      <Combobox
        items={issuerOptions}
        selected={selectedIssuerId}
        onSelectionChange={(issuer) => onChange(issuer.value)}
        onSearchChange={setSearch}
        emptyMessage={emptyMessage}
        listFooter={loadMore}
        searchPlaceholder="Search authorization servers…"
        className="w-full justify-between"
        contentClassName="w-[min(500px,calc(100vw-2rem))]"
      >
        {selectedIssuer
          ? issuerOptionLabel(selectedIssuer)
          : "Choose an authorization server…"}
      </Combobox>
      {pickFailed ? (
        <FieldError>
          Failed to load this authorization server. Pick it again, or refresh
          the page.
        </FieldError>
      ) : (
        <Text muted small>
          Pick an authorization server this project already has: its own, its
          organization&apos;s, or one from the platform catalog.
        </Text>
      )}
    </Stack>
  );
}
