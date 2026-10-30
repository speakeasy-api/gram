import { useMemo } from "react";
import { useNavigate } from "react-router";
import { useOrganization, useSession } from "@/contexts/Auth";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { useRBAC } from "@/hooks/useRBAC";
import { useRBACScopeOverride } from "@/hooks/useRBACScopeOverride";
import {
  hasPlatformSetup,
  platformSetup,
} from "@/pages/agents/credentials/platformSetups";
import { useCatalogEntries } from "@/pages/agents/credentials/workloadCatalog";
import { useRoutes } from "@/routes";
import type { LauncherCandidate } from "./types";

/**
 * One candidate per catalog platform the agent wizard ships a guided setup
 * for, such as GitHub Actions or GKE: it opens the new-agent wizard on that
 * platform, the same deep link the agents list uses. The platforms are the
 * ones the wizard's credential step offers as tiles, so the two cannot
 * disagree. Offered in a project to someone who can manage workload
 * identities, since setting one up trusts the platform for the organization.
 *
 * "In a project" is the palette's own inProject, read from the URL: the
 * palette is mounted above the project's provider, so useProject() there is
 * always the empty project and its id cannot gate anything.
 */
export function usePlatformSetupCandidates({
  enabled,
  inProject = true,
}: {
  enabled: boolean;
  inProject?: boolean;
}): LauncherCandidate[] {
  const organization = useOrganization();
  const session = useSession();
  const scopeOverride = useRBACScopeOverride();
  const { hasScope } = useRBAC();
  const routes = useRoutes();
  const navigate = useNavigate();
  const allowed =
    enabled &&
    inProject &&
    organization.slug !== DEMO_ORG_SLUG &&
    !session.organizationOverride &&
    !session.impersonatorEmail &&
    scopeOverride === null &&
    hasScope("workload:write");
  const { entries } = useCatalogEntries(allowed);
  return useMemo(() => {
    if (!allowed) return [];
    return entries.filter(hasPlatformSetup).map((entry) => ({
      id: `platform-setup:${entry.key}`,
      kind: "page" as const,
      title: `Set up ${entry.displayName}`,
      detail: entry.description,
      keywords: [
        entry.displayName,
        entry.key,
        "workload identity",
        "OIDC",
        ...(platformSetup(entry)?.keywords ?? []),
      ],
      verbs: ["open" as const],
      group: "Identities",
      run: () => {
        void navigate(
          `${routes.identities.agents.new.href()}?platform=${encodeURIComponent(entry.key)}`,
        );
      },
    }));
  }, [allowed, entries, navigate, routes]);
}
