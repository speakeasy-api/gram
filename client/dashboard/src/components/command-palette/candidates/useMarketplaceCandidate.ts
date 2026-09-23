import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";
import { usePublishPluginsMutation } from "@gram/client/react-query/publishPlugins.js";
import {
  invalidateAllPublishStatus,
  usePublishStatus,
} from "@gram/client/react-query/publishStatus.js";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { toast } from "sonner";
import type { LauncherCandidate, Verb } from "./types";

function detailFor(status: PublishStatusResult): string {
  if (!status.connected) return "Plugin marketplace · not connected";
  if (status.upToDate === false) {
    return "Plugin marketplace · unpublished changes";
  }
  return "Plugin marketplace · up to date";
}

function verbsFor(status: PublishStatusResult, isAdmin: boolean): Verb[] {
  return status.configured && status.connected && isAdmin
    ? ["open", "publish"]
    : ["open"];
}

/**
 * The single synthetic, project-level "Plugin marketplace" candidate. Present
 * only inside a project and only once the publish status has resolved, since
 * its detail and verbs are read from that status.
 */
export function useMarketplaceCandidate({
  enabled,
  inProject,
}: {
  enabled: boolean;
  inProject: boolean;
}): LauncherCandidate[] {
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const { hasScope } = useRBAC();
  const isAdmin = hasScope("org:admin");
  const { data: status } = usePublishStatus(undefined, undefined, {
    enabled: enabled && inProject,
  });
  const { mutateAsync: publishPlugins } = usePublishPluginsMutation();

  return useMemo(() => {
    if (!inProject || !status) return [];

    const publish = async () => {
      try {
        // An empty collaborator list is valid: the server only iterates it
        // (server/internal/plugins/impl.go, PublishPlugins). Collaborators are
        // managed from the plugins page, not from the palette.
        await publishPlugins({
          security: { sessionHeaderGramSession: "" },
          request: { publishPluginsRequestBody: { githubUsernames: [] } },
        });
      } catch (error) {
        toast.error("Failed to publish plugins to GitHub");
        throw error;
      }
      await invalidateAllPublishStatus(queryClient);
      toast.success("Plugins published to GitHub");
    };

    const candidate: LauncherCandidate = {
      id: "marketplace",
      kind: "marketplace",
      title: "Plugin marketplace",
      detail: detailFor(status),
      keywords: ["plugins", "marketplace", "github"],
      verbs: verbsFor(status, isAdmin),
      icon: "store",
      group: "Plugins",
      run: (verb) => {
        if (verb === "publish") return publish();
        routes.plugins.goTo();
      },
    };
    return [candidate];
  }, [inProject, status, isAdmin, routes, queryClient, publishPlugins]);
}
