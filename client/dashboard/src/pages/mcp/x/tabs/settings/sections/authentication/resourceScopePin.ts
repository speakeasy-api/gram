import { normalizeScopes } from "@/lib/remote-identity";
import type { RemoteMcpServerScopes } from "@gram/client/models/components/remotemcpserverscopes.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import {
  setGetRemoteMcpServerScopesData,
  useGetRemoteMcpServerScopes,
} from "@gram/client/react-query/getRemoteMcpServerScopes.js";
import { useSetRemoteMcpServerScopePinMutation } from "@gram/client/react-query/setRemoteMcpServerScopePin.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

export type ResourceScopePin = {
  data: RemoteMcpServerScopes | undefined;
  isError: boolean;
  /** The error is a 403: the caller cannot write every server sharing the URL. */
  forbidden: boolean;
  /** The draft pin; the saved one until edited. Empty means no pin. */
  value: string[];
  setValue: (values: string[]) => void;
  dirty: boolean;
  /** Writes the draft. Resolves true when something was written. */
  save: () => Promise<boolean>;
  saving: boolean;
};

function sameScopes(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((scope, i) => scope === b[i]);
}

/** The server's scope pin and an unsaved edit of it. */
export function useResourceScopePin({
  mcpServerId,
  enabled,
}: {
  mcpServerId: string;
  enabled: boolean;
}): ResourceScopePin {
  const queryClient = useQueryClient();
  const query = useGetRemoteMcpServerScopes({ mcpServerId }, undefined, {
    enabled: enabled && mcpServerId !== "",
    throwOnError: false,
  });
  const mutation = useSetRemoteMcpServerScopePinMutation();
  const [draft, setDraft] = useState<string[] | null>(null);
  const saved = query.data?.pinnedScopes ?? [];
  const value = draft ?? saved;
  const dirty = enabled && draft !== null && !sameScopes(draft, saved);

  return {
    data: enabled ? query.data : undefined,
    isError: enabled && query.isError,
    forbidden:
      enabled &&
      query.error instanceof GramError &&
      query.error.statusCode === 403,
    value,
    // Back to the saved pin drops the draft, so refetches show through.
    setValue: (values) => {
      const next = normalizeScopes(values);
      setDraft(sameScopes(next, saved) ? null : next);
    },
    dirty,
    save: async () => {
      if (!dirty) return false;
      const result = await mutation.mutateAsync({
        request: {
          setServerScopePinRequestBody: { mcpServerId, scopes: value },
        },
      });
      setGetRemoteMcpServerScopesData(queryClient, [{ mcpServerId }], result);
      setDraft(null);
      return true;
    },
    saving: mutation.isPending,
  };
}

function connectedEntry(
  scopes: RemoteMcpServerScopes,
  connectedClientId: string | null,
): RemoteMcpServerScopes["clients"][number] | undefined {
  if (!connectedClientId) return undefined;
  return scopes.clients.find((client) => client.clientId === connectedClientId);
}

/**
 * What the pin does for a sign-in through the connected client, flag first.
 * Read from the server's resolution, never re-derived here. `draft` is an
 * unsaved edit, described only where the server says a pin would decide.
 */
export function scopePinStatus(
  scopes: RemoteMcpServerScopes,
  connectedClientId: string | null,
  draft?: string[],
): string[] {
  if (
    draft &&
    scopes.discoveryEnabled &&
    connectedEntry(scopes, connectedClientId)?.pinWouldDecide
  ) {
    return [
      draft.length > 0
        ? "After you save, sign-ins request these scopes."
        : "After you save, sign-ins use the scopes the MCP server advertises, or the identity provider's scopes if it advertises none.",
    ];
  }
  const lines: string[] = [];
  if (!scopes.discoveryEnabled) {
    lines.push(
      "Not used: pinned scopes are not enabled for your organization.",
    );
  }
  if (!connectedClientId) return lines;
  const source = connectedEntry(scopes, connectedClientId)?.scopeSource;
  const pinned = scopes.pinnedScopes.length > 0;
  switch (source) {
    case "client_scope":
      lines.push("Not used: this connection requests its own scopes.");
      break;
    case "challenge_scope":
      lines.push(
        "Not used: the MCP server's last sign-in challenge names the scopes.",
      );
      break;
    case "resource_pin":
      if (pinned) lines.push("Sign-ins request these scopes.");
      break;
    case "cached_resource":
    case "live_resource":
      if (!pinned && scopes.discoveryEnabled) {
        lines.push("Leave empty to use the scopes the MCP server advertises.");
      } else if (pinned && scopes.discoveryEnabled) {
        lines.push("Not used for this connection.");
      }
      break;
    case "issuer_override":
    case "issuer_catalogue":
    case "issuer_omitted":
    case "none":
    case undefined:
      if (pinned && scopes.discoveryEnabled) {
        lines.push("Not used for this connection.");
      }
      break;
  }
  return lines;
}

/** Pinned scopes the MCP server does not advertise, when it would matter. */
export function unadvertisedPinnedScopes(
  scopes: RemoteMcpServerScopes,
  value: string[],
  dirty: boolean,
  connectedClientId: string | null,
): string[] {
  const entry = connectedEntry(scopes, connectedClientId);
  // The server's answer describes the saved pin.
  if (!dirty) return entry?.unadvertisedPinnedScopes ?? [];
  if (!entry || !scopes.discoveryEnabled || !scopes.advertisedScopesKnown) {
    return [];
  }
  // An edit only matters where the server says a pin would decide.
  if (!entry.pinWouldDecide) return [];
  const advertised = scopes.advertisedScopes ?? [];
  return value.filter((scope) => !advertised.includes(scope));
}

/** "Shared with N other MCP server(s) on the same URL." */
export function sharedServerLine(count: number): string | null {
  if (count <= 0) return null;
  return `Shared with ${count} other MCP ${count === 1 ? "server" : "servers"} on the same URL.`;
}
