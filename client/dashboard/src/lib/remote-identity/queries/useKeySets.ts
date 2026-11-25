import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import type { ListJSONWebKeySetsResult } from "@gram/client/models/components/listjsonwebkeysetsresult.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import {
  buildListJsonWebKeySetsQuery,
  invalidateAllListJsonWebKeySets,
} from "@gram/client/react-query/listJsonWebKeySets";
import {
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
import { useCallback } from "react";
import type { KeySetChoice } from "../model/serviceAccount";

/** The organization's key sets, keyed by organization so a switch never offers
 * the previous one's. */
export function useOrganizationKeySets(
  enabled: boolean,
): UseQueryResult<ListJSONWebKeySetsResult> {
  const organization = useOrganization();
  const gramClient = useGramContext();
  const setsQuery = buildListJsonWebKeySetsQuery(gramClient);
  return useQuery({
    ...setsQuery,
    queryKey: [...setsQuery.queryKey, { organizationId: organization.id }],
    enabled,
  });
}

/**
 * Turns a choice into a key set id, creating the set first when the choice is
 * a new one. Creating publishes the KMS key as the set's active key, so the
 * set can sign straight away.
 */
export function useResolveKeySet(): (choice: KeySetChoice) => Promise<string> {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  return useCallback(
    async (choice) => {
      if (choice.kind === "existing") return choice.id;
      const created = await client.jsonWebKeySets.create(
        {
          createJSONWebKeySetForm: {
            name: choice.name.trim(),
            externalKeyId: choice.externalKeyId,
          },
        },
        { sessionHeaderGramSession: "" },
      );
      await invalidateAllListJsonWebKeySets(queryClient);
      return created.id;
    },
    [client, queryClient],
  );
}
