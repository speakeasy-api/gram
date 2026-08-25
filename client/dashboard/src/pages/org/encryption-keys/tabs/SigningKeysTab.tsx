import { Button } from "@/components/ui/Button";
import { DotRow } from "@/components/ui/DotRow";
import { DotTable } from "@/components/ui/DotTable";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import { useOrgRoutes } from "@/routes";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildListJsonWebKeysQuery } from "@gram/client/react-query/listJsonWebKeys";
import { useListJsonWebKeySets } from "@gram/client/react-query/listJsonWebKeySets";
import { useQueries } from "@tanstack/react-query";

// SigningKeysTab lists the signing key sets that depend on this KMS key.
// Deleting the key is refused while any of them does, so this is also where a
// reader finds out what is holding a delete up.
//
// A set depends on the key in two ways, and both block deletion: the set
// publishes new keys from it (the set's own external_key_id), or a key already
// published in the set was minted from it and still signs with it even after
// the set moved on to another KMS key. The second is why every set's keys are
// read rather than only the sets currently pointed at this key.
export function SigningKeysTab({
  externalKeyId,
}: {
  externalKeyId: string;
}): JSX.Element {
  const client = useGramContext();
  const orgRoutes = useOrgRoutes();
  const {
    data: setsData,
    isLoading: setsLoading,
    isError: setsError,
    refetch,
  } = useListJsonWebKeySets();
  const sets = setsData?.sets ?? [];

  const keyQueries = useQueries({
    queries: sets.map((set) =>
      buildListJsonWebKeysQuery(client, { setId: set.id }),
    ),
  });
  const keysLoading = keyQueries.some((query) => query.isLoading);
  const keysError = keyQueries.some((query) => query.isError);

  if (setsError || keysError) {
    return (
      <Stack gap={3} className="py-8" align="center" justify="center">
        <Text muted>Failed to load signing key sets.</Text>
        <Button
          size="sm"
          variant="secondary"
          onClick={() => {
            void refetch();
            for (const query of keyQueries) void query.refetch();
          }}
        >
          <Button.Text>Retry</Button.Text>
        </Button>
      </Stack>
    );
  }

  if (setsLoading || keysLoading) {
    return <Text muted>Loading…</Text>;
  }

  const dependents = sets.flatMap((set, index) => {
    const publishesFrom = set.externalKeyId === externalKeyId;
    const hasPublishedKey = (keyQueries[index]?.data?.keys ?? []).some(
      (key) => key.externalKeyId === externalKeyId,
    );
    if (!publishesFrom && !hasPublishedKey) return [];
    return [{ set, publishesFrom, hasPublishedKey }];
  });

  if (dependents.length === 0) {
    return (
      <Stack gap={2}>
        <Text muted>No signing key sets use this key.</Text>
        <Text small muted>
          Sets that publish from this key, or that have a key published from it,
          appear here. While any of them exists, this key cannot be deleted.
        </Text>
      </Stack>
    );
  }

  const headers = [
    { label: "Name" },
    { label: "Depends on this key as" },
    { label: "Created" },
  ];

  return (
    <div className="flex flex-col gap-4">
      <Text small muted>
        Deleting this key is refused while any of these sets exists, because a
        key published from it could no longer sign.
      </Text>
      <DotTable headers={headers}>
        {dependents.map(({ set, publishesFrom, hasPublishedKey }) => (
          <DotRow
            key={set.id}
            icon={
              <Icon
                name="key-round"
                className="text-muted-foreground h-5 w-5"
              />
            }
            href={orgRoutes.signingKeySets.setDetail.href(set.id)}
            ariaLabel={`View signing key set ${set.name}`}
          >
            <td className="px-3 py-3">
              <Text
                variant="subheading"
                as="div"
                className="group-hover:text-primary truncate text-sm transition-colors group-hover:underline"
              >
                {set.name}
              </Text>
            </td>
            <td className="px-3 py-3">
              <Text small muted>
                {dependencyLabel(publishesFrom, hasPublishedKey)}
              </Text>
            </td>
            <td className="px-3 py-3">
              <Text small muted as="div">
                <HumanizeDateTime date={set.createdAt} />
              </Text>
            </td>
          </DotRow>
        ))}
      </DotTable>
    </div>
  );
}

function dependencyLabel(
  publishesFrom: boolean,
  hasPublishedKey: boolean,
): string {
  if (publishesFrom && hasPublishedKey) {
    return "Publishing key and published keys";
  }
  if (publishesFrom) {
    return "Publishing key";
  }
  return "Published keys";
}
