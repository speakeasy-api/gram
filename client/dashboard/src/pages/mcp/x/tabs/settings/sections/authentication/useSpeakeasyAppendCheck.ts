import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import type { EMAProviderOrder } from "@gram/client/models/components/emaproviderorder.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildAddOrganizationRemoteSessionIssuerSpeakeasyEMAProviderMutation } from "@gram/client/react-query/addOrganizationRemoteSessionIssuerSpeakeasyEMAProvider.js";

/** Runs the server's checks for appending Speakeasy without saving. */
export function useSpeakeasyAppendCheck(
  target: { remoteSessionIssuerId: string; userSessionIssuerId: string },
  enabled: boolean,
): UseQueryResult<EMAProviderOrder, Error> {
  const core = useGramContext();
  return useQuery({
    queryKey: ["identityChaining", "speakeasyAppendCheck", target],
    enabled,
    retry: false,
    queryFn: ({ signal }) =>
      buildAddOrganizationRemoteSessionIssuerSpeakeasyEMAProviderMutation(
        core,
      ).mutationFn({
        request: {
          addSpeakeasyEMAProviderRequestBody: { ...target, dryRun: true },
        },
        options: { fetchOptions: { signal } },
      }),
  });
}
