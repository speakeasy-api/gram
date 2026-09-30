import { useRBAC } from "@/hooks/useRBAC";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import {
  invalidateListAPIKeys,
  useListAPIKeys,
} from "@gram/client/react-query/listAPIKeys";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

const AGENT_SCOPE = "agent";

export type UseAgentToken = {
  /** The secret minted this session, or null. Returned only once at creation. */
  generatedToken: string | null;
  /** Whether the post-generate clipboard write succeeded. */
  autoCopied: boolean;
  /** True while the create request is in flight. */
  isPending: boolean;
  /** True if the create request failed. */
  isError: boolean;
  /** Whether the caller may mint keys (requires the org:admin scope). */
  canGenerate: boolean;
  /** Whether an agent key already exists — so the action re-generates. */
  hasExistingAgentKey: boolean;
  /** Mint an additional agent key. Existing agent keys keep working. */
  generate: () => void;
};

/**
 * useAgentToken mints the org's `agent`-scoped API key, i.e. the device agent's
 * `org_token`. On a successful mint it best-effort copies a caller-built
 * payload (e.g. a ready-to-paste managed.json) to the clipboard.
 *
 * Minting never revokes existing agent keys: devices already deployed with an
 * older token keep syncing. Retire old tokens explicitly under Settings → API
 * Keys.
 */
export function useAgentToken(opts: {
  /** Builds the text to copy once a token is minted (e.g. a managed.json). */
  buildCopyText: (token: string) => string;
}): UseAgentToken {
  const { buildCopyText } = opts;
  const queryClient = useQueryClient();

  // Gate on the org:admin scope, matching the API Keys page.
  const { hasAnyScope } = useRBAC();
  const canGenerate = hasAnyScope(["org:admin"]);

  // Listing keys needs org:admin, so only fetch when the user can act on it.
  const { data: keysData } = useListAPIKeys(undefined, undefined, {
    enabled: canGenerate,
  });
  const hasExistingAgentKey = (keysData?.keys ?? []).some((k) =>
    k.scopes.includes(AGENT_SCOPE),
  );

  const [generatedToken, setGeneratedToken] = useState<string | null>(null);
  const [autoCopied, setAutoCopied] = useState(false);

  const createKeyMutation = useCreateAPIKeyMutation({
    onSuccess: async (data) => {
      if (!data.key) return;
      setGeneratedToken(data.key);
      // Best-effort: drop the payload straight onto the clipboard. Clipboard
      // writes need transient user activation, which a slow request can outlive,
      // so this may reject — the caller can still surface a manual copy path.
      try {
        await navigator.clipboard.writeText(buildCopyText(data.key));
        setAutoCopied(true);
      } catch {
        setAutoCopied(false);
      }
      // Refresh the cached key list so it reflects the new key.
      await invalidateListAPIKeys(queryClient, [{ gramSession: "" }]);
    },
  });

  const generate = () => {
    createKeyMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        createKeyForm: {
          // Unique per mint (to the second): the (org, name) unique index would
          // otherwise reject a second key minted the same day.
          name: `device-agent ${new Date()
            .toISOString()
            .slice(0, 19)
            .replace("T", " ")}`,
          scopes: [AGENT_SCOPE],
        },
      },
    });
  };

  return {
    generatedToken,
    autoCopied,
    isPending: createKeyMutation.isPending,
    isError: createKeyMutation.isError,
    canGenerate,
    hasExistingAgentKey,
    generate,
  };
}
