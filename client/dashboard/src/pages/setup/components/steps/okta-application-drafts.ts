import { useCallback, useMemo, useState } from "react";
import type { IdentityProviderApplication } from "@gram/client/models/components/identityproviderapplication.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import { invalidateAllMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import {
  invalidateAllMcpServers,
  useMcpServers,
} from "@gram/client/react-query/mcpServers.js";
import { invalidateAllRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { invalidateAllRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import { invalidateAllUserSessionIssuers } from "@gram/client/react-query/userSessionIssuers.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  useProjectSlugForRequests,
  useSdkClient,
  useSlugs,
} from "@/contexts/Sdk";
import {
  DEFAULT_ENDPOINT_FAILED_MESSAGE,
  createDefaultMcpEndpoint,
} from "@/lib/mcpEndpoints";
import { persistServerIconBestEffort } from "@/lib/mcpServerIcon";
import { createRemoteMcpServerPair } from "@/lib/remoteMcpServers";
import { mcpServerRouteParam, validateMcpServerUrl } from "@/lib/sources";
import {
  configureDraftIdentity,
  type DraftIdentityOutcome,
} from "./configure-draft-identity";

/** The two rows one draft is made of, kept so its sign-in can be tried again. */
export interface DraftPair {
  remoteMcpServer: RemoteMcpServer;
  mcpServer: McpServer;
}

/** What became of one application's draft, as its card reports it. */
export type DraftOutcome =
  | { status: "creating" }
  | {
      status: "created";
      mcpServerParam: string;
      pair: DraftPair;
      identity: DraftIdentityOutcome;
      /**
       * The default endpoint could not be staged, so the server exists but
       * serves nothing until someone adds one. Never a failure of the draft:
       * the endpoint is a convenience and the server stands without it.
       */
      endpointFailed?: boolean;
    }
  | { status: "exists"; mcpServerParam: string }
  | { status: "failed"; message: string };

export interface ApplicationDrafts {
  /** Keyed by source application id. */
  outcomes: Record<string, DraftOutcome>;
  /** The project the drafts are created in, and whose page they link to. */
  projectSlug: string;
  creating: boolean;
  /**
   * Which of the picked applications the run is on, while a whole selection is
   * being created. Absent for a single card's own retry, which has no run to
   * count through.
   */
  progress: { current: number; total: number } | undefined;
  /** Creates one draft per application, in the order given. */
  create: (applications: IdentityProviderApplication[]) => Promise<void>;
  /** Runs the sign-in provider steps again for a draft that already exists. */
  configureIdentity: (
    sourceApplicationId: string,
    pair: DraftPair,
  ) => Promise<void>;
}

function failureMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/**
 * Creates one private MCP server per Okta application, backed by a remote MCP
 * server at the endpoint the catalog match names, and carries each one on to
 * the sign-in provider its upstream asks for.
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
  // The endpoint slug carries the organization, the same as every other path.
  const { orgSlug } = useSlugs();
  const existing = useMcpServers({ gramProject: projectSlug }, undefined, {
    throwOnError: false,
  });

  const [outcomes, setOutcomes] = useState<Record<string, DraftOutcome>>({});
  const [creating, setCreating] = useState(false);
  const [progress, setProgress] = useState<{
    current: number;
    total: number;
  }>();

  const existingByName = useMemo(() => {
    const byName = new Map<string, string>();
    for (const server of existing.data?.mcpServers ?? []) {
      const name = server.name?.trim().toLowerCase();
      if (name) byName.set(name, mcpServerRouteParam(server));
    }
    return byName;
  }, [existing.data]);

  const record = useCallback((id: string, outcome: DraftOutcome) => {
    setOutcomes((previous) => ({ ...previous, [id]: outcome }));
  }, []);

  // The sign-in provider steps, from the probe to the committed client. They
  // run after the server exists and can never take it back: whatever they
  // report, the draft stands.
  const runIdentity = useCallback(
    async (id: string, pair: DraftPair, endpointFailed = false) => {
      const mcpServerParam = mcpServerRouteParam(pair.mcpServer);
      const created = (identity: DraftIdentityOutcome): DraftOutcome => ({
        status: "created",
        mcpServerParam,
        pair,
        identity,
        endpointFailed,
      });

      record(id, created({ status: "configuring" }));
      const identity = await configureDraftIdentity({
        client,
        remoteMcpServer: pair.remoteMcpServer,
        mcpServer: pair.mcpServer,
      });
      record(id, created(identity));

      // A committed provider mints a client and links the server's own issuer,
      // so those lists go stale too.
      if (identity.status === "configured") {
        await Promise.all([
          invalidateAllRemoteSessionIssuers(queryClient, {
            refetchType: "all",
          }),
          invalidateAllRemoteSessionClients(queryClient, {
            refetchType: "all",
          }),
          invalidateAllUserSessionIssuers(queryClient, { refetchType: "all" }),
        ]);
      }
    },
    [client, queryClient, record],
  );

  const create = useCallback(
    async (applications: IdentityProviderApplication[]) => {
      setCreating(true);

      try {
        for (const [index, application] of applications.entries()) {
          setProgress({ current: index + 1, total: applications.length });
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
          let pair: DraftPair;
          try {
            pair = await createRemoteMcpServerPair(client, {
              name: application.label,
              url: remoteUrl,
              visibility: "private",
            });
          } catch (error) {
            record(id, { status: "failed", message: failureMessage(error) });
            continue;
          }

          // Okta already knows what each application looks like, so a draft
          // arrives with the logo its people recognize rather than initials.
          // Best-effort by design: a logo is not worth failing a server over.
          await persistServerIconBestEffort(
            client,
            application.logoUrl,
            pair.mcpServer.id,
          );

          // Without an endpoint the server exists but serves nothing, which is
          // the state a reader reads as broken. Best-effort like every other
          // create path: the server stands whether or not this lands.
          let endpointFailed = false;
          if (orgSlug) {
            endpointFailed = !(await createDefaultMcpEndpoint(
              client,
              pair.mcpServer,
              orgSlug,
            ));
          } else {
            endpointFailed = true;
            toast.warning(DEFAULT_ENDPOINT_FAILED_MESSAGE);
          }

          // refetchType "all" forces the refetch even with no active observer:
          // none of these lists is mounted behind the setup flow, and they must
          // carry the draft by the time the reader reaches them.
          await Promise.all([
            invalidateAllRemoteMcpServers(queryClient, { refetchType: "all" }),
            invalidateAllMcpServers(queryClient, { refetchType: "all" }),
            invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
          ]);

          await runIdentity(id, pair, endpointFailed);
        }
      } finally {
        setCreating(false);
        setProgress(undefined);
      }
    },
    [client, existingByName, orgSlug, queryClient, record, runIdentity],
  );

  const configureIdentity = useCallback(
    async (sourceApplicationId: string, pair: DraftPair) => {
      setCreating(true);
      try {
        await runIdentity(sourceApplicationId, pair);
      } finally {
        setCreating(false);
      }
    },
    [runIdentity],
  );

  return {
    outcomes,
    projectSlug,
    creating,
    progress,
    create,
    configureIdentity,
  };
}
