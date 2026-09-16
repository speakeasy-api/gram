import { useCallback, useMemo, useState } from "react";
import type { IdentityProviderApplication } from "@gram/client/models/components/identityproviderapplication.js";
import { invalidateAllMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import {
  invalidateAllMcpServers,
  useMcpServers,
} from "@gram/client/react-query/mcpServers.js";
import { invalidateAllRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";
import { useQueryClient } from "@tanstack/react-query";
import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";
import { createRemoteMcpServerPair } from "@/lib/remoteMcpServers";
import { mcpServerRouteParam, validateMcpServerUrl } from "@/lib/sources";

/** What became of one application's draft, as its card reports it. */
export type DraftOutcome =
  | { status: "creating" }
  | { status: "created"; mcpServerParam: string }
  | { status: "exists"; mcpServerParam: string }
  | { status: "failed"; message: string };

export interface ApplicationDrafts {
  /** Keyed by source application id. */
  outcomes: Record<string, DraftOutcome>;
  /** The project the drafts are created in, and whose page they link to. */
  projectSlug: string;
  creating: boolean;
  /** Creates one draft per application, in the order given. */
  create: (applications: IdentityProviderApplication[]) => Promise<void>;
}

function failureMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/**
 * Creates one private MCP server per Okta application, backed by a remote MCP
 * server at the endpoint the catalog match names.
 *
 * Created drafts are remembered for as long as the step is open, so pressing
 * the bar again with the same cards still picked does not make a second copy;
 * an application whose name the project already carries is left alone for the
 * same reason, and says so on its card.
 */
export function useApplicationDrafts(): ApplicationDrafts {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  // The setup flow is organization-scoped, so there is no project in the path:
  // this is the project the SDK is already sending every request to, which is
  // the one the drafts land in and the one their links have to point at.
  const projectSlug = useProjectSlugForRequests();
  const existing = useMcpServers({ gramProject: projectSlug }, undefined, {
    throwOnError: false,
  });

  const [outcomes, setOutcomes] = useState<Record<string, DraftOutcome>>({});
  const [creating, setCreating] = useState(false);

  const existingByName = useMemo(() => {
    const byName = new Map<string, string>();
    for (const server of existing.data?.mcpServers ?? []) {
      const name = server.name?.trim().toLowerCase();
      if (name) byName.set(name, mcpServerRouteParam(server));
    }
    return byName;
  }, [existing.data]);

  const create = useCallback(
    async (applications: IdentityProviderApplication[]) => {
      setCreating(true);
      const record = (id: string, outcome: DraftOutcome) =>
        setOutcomes((previous) => ({ ...previous, [id]: outcome }));

      try {
        for (const application of applications) {
          const id = application.sourceApplicationId;
          const remoteUrl = application.match?.remoteUrl;
          if (!remoteUrl) continue;

          // The same check every "add an MCP server by URL" surface makes. The
          // endpoint comes from the catalog rather than from a reader, so this
          // catching anything means the catalog entry itself is unusable — said
          // on the card rather than sent to the server to be refused.
          const invalid = validateMcpServerUrl(remoteUrl);
          if (invalid) {
            record(id, {
              status: "failed",
              message: `Speakeasy has no usable endpoint for this application: ${invalid}.`,
            });
            continue;
          }

          const alreadyInProject = existingByName.get(
            application.label.trim().toLowerCase(),
          );
          if (alreadyInProject) {
            record(id, {
              status: "exists",
              mcpServerParam: alreadyInProject,
            });
            continue;
          }

          record(id, { status: "creating" });
          try {
            const { mcpServer } = await createRemoteMcpServerPair(client, {
              name: application.label,
              url: remoteUrl,
              visibility: "private",
            });
            record(id, {
              status: "created",
              mcpServerParam: mcpServerRouteParam(mcpServer),
            });
          } catch (error) {
            record(id, { status: "failed", message: failureMessage(error) });
            continue;
          }

          // refetchType "all" forces the refetch even with no active observer:
          // none of these lists is mounted behind the setup flow, and they must
          // carry the draft by the time the reader reaches them.
          await Promise.all([
            invalidateAllRemoteMcpServers(queryClient, { refetchType: "all" }),
            invalidateAllMcpServers(queryClient, { refetchType: "all" }),
            invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
          ]);
        }
      } finally {
        setCreating(false);
      }
    },
    [client, existingByName, queryClient],
  );

  return { outcomes, projectSlug, creating, create };
}
