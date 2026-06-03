import { useRBAC } from "@/hooks/useRBAC";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import { useGramContext } from "@gram/client/react-query";
import {
  buildListAPIKeysQuery,
  invalidateListAPIKeys,
  useListAPIKeys,
} from "@gram/client/react-query/listAPIKeys";
import { useRevokeAPIKeyMutation } from "@gram/client/react-query/revokeAPIKey";
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
  /** Whether an agent key already exists — so the action rotates, not creates. */
  hasExistingAgentKey: boolean;
  /** Mint a fresh agent key (rotating: revokes prior agent keys on success). */
  generate: () => void;
};

/**
 * useAgentToken mints — and rotates — the org's `agent`-scoped API key, i.e.
 * the device agent's `org_token`. On a successful mint it best-effort copies a
 * caller-built payload (e.g. a ready-to-paste managed.json) to the clipboard,
 * then revokes any prior agent key(s). Create runs before revoke so a failed
 * create never leaves the org without an agent key.
 *
 * Lives as a hook (rather than inline in the page) so the rotation invariant is
 * testable in isolation and reusable by other surfaces that mint agent keys.
 */
export function useAgentToken(opts: {
  /** Builds the text to copy once a token is minted (e.g. a managed.json). */
  buildCopyText: (token: string) => string;
}): UseAgentToken {
  const { buildCopyText } = opts;
  const queryClient = useQueryClient();
  const client = useGramContext();

  // Gate on the org:admin *scope* (RBAC), matching the API Keys page. When RBAC
  // isn't enabled (local dev / non-enterprise) hasAnyScope returns true.
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

  const revokeKeyMutation = useRevokeAPIKeyMutation();
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
      // Rotation: now that a fresh key exists, revoke the prior agent key(s).
      // Revoking is a soft-delete; the partial unique index on (org, name) is
      // WHERE deleted IS FALSE, so it also frees their names.
      //
      // Fetch the authoritative list rather than trusting the component's
      // cached keysData: that query may not have loaded (or may be mid-refetch)
      // when this fires, and an empty snapshot would silently skip rotation,
      // leaving the old key(s) live. Exclude the key we just minted (data.id),
      // which the fresh list now includes, so we never revoke it.
      let stale = (keysData?.keys ?? []).filter(
        (k) => k.id !== data.id && k.scopes.includes(AGENT_SCOPE),
      );
      try {
        const fresh = await queryClient.fetchQuery(
          buildListAPIKeysQuery(client),
        );
        stale = (fresh.keys ?? []).filter(
          (k) => k.id !== data.id && k.scopes.includes(AGENT_SCOPE),
        );
      } catch {
        // Fall back to cached keysData if the refresh fails.
      }
      for (const k of stale) {
        revokeKeyMutation.mutate({
          security: { sessionHeaderGramSession: "" },
          request: { id: k.id },
        });
      }
      // Refresh the cached key list so it reflects the new key + revocations.
      await invalidateListAPIKeys(queryClient, [{ gramSession: "" }]);
    },
  });

  const generate = () => {
    createKeyMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        createKeyForm: {
          // Unique per mint (to the second): the (org, name) unique index would
          // otherwise reject a same-day re-create, and create runs before the
          // old key is revoked.
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
