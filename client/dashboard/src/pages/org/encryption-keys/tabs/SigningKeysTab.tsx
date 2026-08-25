import { Button } from "@/components/ui/Button";
import { DotRow } from "@/components/ui/DotRow";
import { DotTable } from "@/components/ui/DotTable";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { HumanizeDateTime } from "@/lib/dates";
import { useOrgRoutes } from "@/routes";
import type { JSONWebKeySet } from "@gram/client/models/components/jsonwebkeyset.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildListJsonWebKeysQuery } from "@gram/client/react-query/listJsonWebKeys";
import { buildListJsonWebKeySetsQuery } from "@gram/client/react-query/listJsonWebKeySets";
import { useQueries, useQuery } from "@tanstack/react-query";

// How a signing key set relates to this KMS key. A set can publish new keys
// from it, hold a key already published from it (which keeps signing with it
// even after the set moves on to another KMS key), or only hold keys that were
// published from it and since revoked. The first two block deleting the KMS
// key; a revoked key is withdrawn and no longer needs it.
type Dependent = {
  set: JSONWebKeySet;
  publishesFrom: boolean;
  hasLiveKey: boolean;
  hasRevokedKey: boolean;
};

function dependencyLabel(dependent: Dependent): string {
  const parts: string[] = [];
  if (dependent.publishesFrom) parts.push("Publishing key");
  if (dependent.hasLiveKey) parts.push("Published keys");
  if (dependent.hasRevokedKey) parts.push("Revoked keys");
  return parts.join(", ");
}

function blocksDeletion(dependent: Dependent): boolean {
  return dependent.publishesFrom || dependent.hasLiveKey;
}

// SigningKeysTab lists the signing key sets that have ever depended on this KMS
// key, revoked keys included, and says which of them still block deleting it.
// Every set's keys are read rather than only the sets currently pointed at this
// key, because a published key pins the KMS key it was minted from.
export function SigningKeysTab({
  externalKeyId,
}: {
  externalKeyId: string;
}): JSX.Element {
  const client = useGramContext();
  const orgRoutes = useOrgRoutes();
  const organization = useOrganization();
  // Keyed by organization so a switch never shows the previous one's sets.
  const setsQuery = buildListJsonWebKeySetsQuery(client);
  const {
    data: setsData,
    isPending: setsPending,
    isError: setsError,
    refetch,
  } = useQuery({
    ...setsQuery,
    queryKey: [...setsQuery.queryKey, { organizationId: organization.id }],
  });
  const sets = setsData?.sets ?? [];

  const keyQueries = useQueries({
    queries: sets.map((set) => {
      const keysQuery = buildListJsonWebKeysQuery(client, {
        setId: set.id,
        includeRevoked: true,
      });
      return {
        ...keysQuery,
        queryKey: [...keysQuery.queryKey, { organizationId: organization.id }],
      };
    }),
  });
  const keysPending = keyQueries.some((query) => query.isPending);
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

  if (setsPending || keysPending) {
    return <Text muted>Loading…</Text>;
  }

  const dependents: Dependent[] = sets.flatMap((set, index) => {
    const keys = (keyQueries[index]?.data?.keys ?? []).filter(
      (key) => key.externalKeyId === externalKeyId,
    );
    const dependent: Dependent = {
      set,
      publishesFrom: set.externalKeyId === externalKeyId,
      hasLiveKey: keys.some((key) => key.keyState !== "revoked"),
      hasRevokedKey: keys.some((key) => key.keyState === "revoked"),
    };
    const related =
      dependent.publishesFrom ||
      dependent.hasLiveKey ||
      dependent.hasRevokedKey;
    return related ? [dependent] : [];
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
    { label: "Blocks deletion" },
    { label: "Created" },
  ];

  return (
    <div className="flex flex-col gap-4">
      <Text small muted>
        Deleting this key is refused while a set publishes from it or still has
        a key published from it, because that key could no longer sign. A set
        that only holds revoked keys from it does not block deletion.
      </Text>
      <DotTable headers={headers}>
        {dependents.map((dependent) => (
          <DotRow
            key={dependent.set.id}
            icon={
              <Icon
                name="key-round"
                className="text-muted-foreground h-5 w-5"
              />
            }
            href={orgRoutes.signingKeySets.setDetail.href(dependent.set.id)}
            ariaLabel={`View signing key set ${dependent.set.name}`}
          >
            <td className="px-3 py-3">
              <Text
                variant="subheading"
                as="div"
                className="group-hover:text-primary truncate text-sm transition-colors group-hover:underline"
              >
                {dependent.set.name}
              </Text>
            </td>
            <td className="px-3 py-3">
              <Text small muted>
                {dependencyLabel(dependent)}
              </Text>
            </td>
            <td className="px-3 py-3">
              <Text small muted>
                {blocksDeletion(dependent) ? "Yes" : "No"}
              </Text>
            </td>
            <td className="px-3 py-3">
              <Text small muted as="div">
                <HumanizeDateTime date={dependent.set.createdAt} />
              </Text>
            </td>
          </DotRow>
        ))}
      </DotTable>
    </div>
  );
}
