import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useOrgRoutes } from "@/routes";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildListExternalKeysQuery } from "@gram/client/react-query/listExternalKeys";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { providerSlug } from "../providers";

// ExternalKeyLink names the KMS key behind a set or a published key and links to
// it. Sets and keys carry only the key's id, so the name is resolved from the
// organization's key list; one query serves every row of a table.
//
// A key that reads back absent is the deleted case: the server's delete guard
// refuses while a live set or key references it, so this is only reachable for
// a revoked key's minting key, but it is still worth saying rather than
// rendering a link to a page that will bounce.
export function ExternalKeyLink({
  externalKeyId,
}: {
  externalKeyId: string;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const client = useGramContext();
  const organization = useOrganization();
  // Keyed by organization so a switch never shows the previous one's keys.
  const keysQuery = buildListExternalKeysQuery(client, { provider: "gcp_kms" });
  const { data, isPending, isError } = useQuery({
    ...keysQuery,
    queryKey: [...keysQuery.queryKey, { organizationId: organization.id }],
    throwOnError: false,
  });

  if (isPending) {
    return (
      <Text small muted as="span">
        Loading…
      </Text>
    );
  }

  // A lookup that failed says nothing about whether the key exists, so it must
  // not be reported as deleted.
  if (isError) {
    return (
      <Text small muted as="span">
        Could not load encryption key
      </Text>
    );
  }

  const externalKey = data.keys.find((key) => key.id === externalKeyId);
  if (!externalKey) {
    return (
      <Text small muted as="span">
        No longer available
      </Text>
    );
  }

  return (
    <Link
      to={orgRoutes.encryptionKeys.keyDetail.href(
        providerSlug(externalKey.provider),
        externalKey.id,
      )}
      className="underline underline-offset-2"
    >
      <Text small as="span">
        {externalKey.name}
      </Text>
    </Link>
  );
}
