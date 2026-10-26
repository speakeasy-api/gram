import { useMemo } from "react";
import { useNavigate } from "react-router";
import {
  useOrganization,
  useSession,
  useIsPlatformAdmin,
  useProject,
} from "@/contexts/Auth";
import { getRBACScopeOverrideHeader } from "@/components/dev-toolbar-utils";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { useRBAC } from "@/hooks/useRBAC";
import { useOrgRoutes, useRoutes } from "@/routes";
import type { LauncherCandidate } from "./types";

export function useInferenceSetupCandidate({
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
  const orgRoutes = useOrgRoutes();
  const navigate = useNavigate();
  const canManageRoles = hasScope("org:admin", organization.id);
  const canConfigureWorkloads = hasScope("workload:write");
  const allowed =
    organization.slug !== DEMO_ORG_SLUG &&
    !session.organizationOverride &&
    !session.impersonatorEmail &&
    getRBACScopeOverrideHeader(import.meta.env.DEV || isPlatformAdmin) ===
      null &&
    enabled;
  return useMemo(() => {
    if (!allowed) return [];
    const candidates: LauncherCandidate[] = [];
    if (inProject && project.id && (canConfigureWorkloads || canManageRoles)) {
      candidates.push({
        id: "inference-workload-setup",
        kind: "page",
        title: "Set up inference with platform tokens",
        detail:
          "Trust an external JWT issuer and grant project-scoped inference",
        keywords: [
          "inference",
          "platform token",
          "workload identity",
          "workload",
          "JWT",
          "OIDC",
          "federation",
        ],
        verbs: ["open"],
        group: "Identities",
        run: () => {
          void navigate(
            `${routes.identities.agents.new.href()}?capability=inference`,
          );
        },
      });
    }
    if (canManageRoles) {
      candidates.push({
        id: "inference-role-permissions",
        kind: "page",
        title: "Configure inference permissions",
        detail:
          "Roles & Permissions · grant project-scoped inference to an owner",
        keywords: [
          "inference",
          "inference:execute",
          "roles",
          "permissions",
          "owner",
        ],
        verbs: ["open"],
        group: "Organization",
        run: () => {
          void navigate(orgRoutes.access.roles.href());
        },
      });
    }
    return candidates;
  }, [
    allowed,
    inProject,
    project.id,
    canConfigureWorkloads,
    canManageRoles,
    navigate,
    routes,
    orgRoutes,
  ]);
}
