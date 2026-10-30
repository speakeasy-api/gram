import { useMemo } from "react";
import { useNavigate } from "react-router";
import {
  useIsPlatformAdmin,
  useOrganization,
  useProject,
  useSession,
} from "@/contexts/Auth";
import { getRBACScopeOverrideHeader } from "@/components/dev-toolbar-utils";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import { useWorkloadPlatforms } from "@gram/client/react-query/workloadPlatforms.js";
import type { LauncherCandidate } from "./types";

/** The catalog key of the GitHub Actions platform. */
const GITHUB_ACTIONS = "github-actions";

/**
 * The GitHub Actions guided setup: the new-agent wizard opened on the
 * platform, which trusts GitHub and admits a repository. Offered to someone
 * who can manage workload identities, in a project, while the catalog still
 * lists the platform as enabled, which is the same gate the wizard's tile uses.
 */
export function useGitHubActionsSetupCandidate({
  enabled,
  inProject = true,
}: {
  enabled: boolean;
  inProject?: boolean;
}): LauncherCandidate[] {
  const project = useProject();
  const organization = useOrganization();
  const session = useSession();
  const isPlatformAdmin = useIsPlatformAdmin();
  const { hasScope } = useRBAC();
  const routes = useRoutes();
  const navigate = useNavigate();
  const allowed =
    enabled &&
    inProject &&
    !!project.id &&
    organization.slug !== DEMO_ORG_SLUG &&
    !session.organizationOverride &&
    !session.impersonatorEmail &&
    getRBACScopeOverrideHeader(import.meta.env.DEV || isPlatformAdmin) ===
      null &&
    hasScope("workload:write");
  const platforms = useWorkloadPlatforms({}, undefined, {
    enabled: allowed,
    throwOnError: false,
  });
  const offered =
    platforms.data?.platforms.some(
      (platform) => platform.key === GITHUB_ACTIONS && platform.enabled,
    ) ?? false;
  return useMemo(() => {
    if (!allowed || !offered) return [];
    return [
      {
        id: "github-actions-workload-setup",
        kind: "page",
        title: "Set up GitHub Actions",
        detail:
          "Let jobs in a GitHub repository act as an agent, with no stored secret",
        keywords: [
          "GitHub",
          "GitHub Actions",
          "CI",
          "workflow",
          "workload",
          "OIDC",
          "device agent",
        ],
        verbs: ["open"],
        group: "Identities",
        run: () => {
          void navigate(
            `${routes.identities.agents.new.href()}?platform=${GITHUB_ACTIONS}`,
          );
        },
      },
    ];
  }, [allowed, offered, navigate, routes]);
}
