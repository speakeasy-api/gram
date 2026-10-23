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
import { useRoutes } from "@/routes";
import type { LauncherCandidate } from "./types";

export function useInferenceSetupCandidate({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const project = useProject();
  const organization = useOrganization();
  const session = useSession();
  const isPlatformAdmin = useIsPlatformAdmin();
  const { hasScope } = useRBAC();
  const routes = useRoutes();
  const navigate = useNavigate();
  const allowed =
    organization.slug !== DEMO_ORG_SLUG &&
    !session.organizationOverride &&
    !session.impersonatorEmail &&
    getRBACScopeOverrideHeader(import.meta.env.DEV || isPlatformAdmin) ===
      null &&
    enabled &&
    !!project.id &&
    hasScope("workload:write") &&
    hasScope("inference:execute", "*", project.id);
  return useMemo(
    () =>
      !allowed
        ? []
        : [
            {
              id: "inference-workload-setup",
              kind: "page",
              title: "Set up inference workload identity",
              detail:
                "Trust an external JWT issuer and grant project-scoped inference",
              keywords: ["inference", "workload", "JWT", "OIDC", "federation"],
              verbs: ["open"],
              group: "Identities",
              run: () => {
                void navigate(
                  `${routes.identities.agents.new.href()}?capability=inference`,
                );
              },
            },
          ],
    [allowed, navigate, routes],
  );
}
