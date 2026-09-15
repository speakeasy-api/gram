import { Button } from "@/components/ui/Button";
import { DotRow } from "@/components/ui/DotRow";
import { DotTable } from "@/components/ui/DotTable";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import { useOrgRoutes } from "@/routes";
import { useListExternalKeys } from "@gram/client/react-query/listExternalKeys";
import { providerSlug } from "../../encryption-keys/providers";

// KmsKeysTab lists the keys this credential reaches. Deleting a credential is
// refused while any of them is live, so this is also where a reader finds out
// what is holding a delete up.
//
// No dedicated endpoint: the list result already carries external_credential_id,
// so filtering client-side avoids a query that would return the same rows.
export function KmsKeysTab({
  credentialId,
}: {
  credentialId: string;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const { data, isLoading, isError, refetch } = useListExternalKeys({
    provider: "gcp_kms",
  });

  const keys = (data?.keys ?? []).filter(
    (key) => key.externalCredentialId === credentialId,
  );

  if (isError) {
    return (
      <Stack gap={3} className="py-8" align="center" justify="center">
        <Text muted>Failed to load encryption keys.</Text>
        <Button size="sm" variant="secondary" onClick={() => void refetch()}>
          <Button.Text>Retry</Button.Text>
        </Button>
      </Stack>
    );
  }

  if (isLoading) {
    return <Text muted>Loading…</Text>;
  }

  if (keys.length === 0) {
    return (
      <Stack gap={2}>
        <Text muted>No encryption keys use this credential.</Text>
        <Text small muted>
          Keys backed by this credential appear here. While any of them exists,
          this credential cannot be deleted.
        </Text>
      </Stack>
    );
  }

  const headers = [
    { label: "Name" },
    { label: "Algorithm" },
    { label: "Created" },
  ];

  return (
    <div className="flex flex-col gap-4">
      <Text small muted>
        Deleting this credential is refused while any of these keys exists,
        because they would have no way to reach your KMS afterwards.
      </Text>
      <DotTable headers={headers}>
        {keys.map((key) => (
          <DotRow
            key={key.id}
            icon={
              <Icon
                name="key-square"
                className="text-muted-foreground h-5 w-5"
              />
            }
            href={orgRoutes.encryptionKeys.keyDetail.href(
              providerSlug(key.provider),
              key.id,
            )}
            ariaLabel={`View encryption key ${key.name}`}
          >
            <td className="px-3 py-3">
              <Text
                variant="subheading"
                as="div"
                className="group-hover:text-primary truncate text-sm transition-colors group-hover:underline"
              >
                {key.name}
              </Text>
            </td>
            <td className="px-3 py-3">
              <Text small muted>
                {key.algorithm}
              </Text>
            </td>
            <td className="px-3 py-3">
              <Text small muted as="div">
                <HumanizeDateTime date={key.createdAt} />
              </Text>
            </td>
          </DotRow>
        ))}
      </DotTable>
    </div>
  );
}
