import { useEffect, useRef } from "react";
import { useOrganization, useUser } from "@/contexts/Auth";

import { Button } from "@/components/ui/Button";
import { Link } from "react-router";
import { createDismissedCtaStore } from "@/hooks/useDismissedCtaStore";
import { useOrgRoutes } from "@/routes";
import { useOrganizationPlatformMCPOnboarding } from "@/hooks/useOrganizationPlatformMCPOnboarding";
import { useRBAC } from "@/hooks/useRBAC";
import { useTelemetry } from "@/contexts/Telemetry";

const dismissedStore = createDismissedCtaStore(
  "gram:platform-mcp-member-promotion:v1",
);

export function MemberPlatformMCPCta(): JSX.Element | null {
  const organization = useOrganization();
  const user = useUser();
  const routes = useOrgRoutes();
  const telemetry = useTelemetry();
  const recordedImpression = useRef<string | null>(null);
  const { hasScope, isLoading, error } = useRBAC();
  const eligible =
    !isLoading &&
    !error &&
    !hasScope("org:admin") &&
    (hasScope("project:read") ||
      hasScope("skill:read") ||
      hasScope("mcp:read"));
  const onboarding = useOrganizationPlatformMCPOnboarding(organization.id, {
    enabled: eligible,
    throwOnError: false,
  });
  const key =
    user.id && organization.id ? `${user.id}:${organization.id}` : undefined;
  const dismissed = dismissedStore.useDismissed(key);
  const visible =
    !!key &&
    eligible &&
    !dismissed &&
    onboarding.data?.enabled &&
    !(
      onboarding.data.connectionAuthorized &&
      onboarding.data.connectionAuthState === "active"
    ) &&
    !onboarding.isError;
  useEffect(() => {
    if (!visible || recordedImpression.current === key) return;
    recordedImpression.current = key;
    telemetry.capture("platform_mcp_member_cta", {
      action: "impression",
      workflow: "organization_home",
    });
  }, [key, visible, telemetry]);
  if (!visible) return null;

  return (
    <div className="border-border bg-card mx-auto mt-6 flex w-full max-w-7xl flex-wrap items-center justify-between gap-4 border px-6 py-4">
      <div>
        <p className="font-medium">Use Speakeasy from your agent</p>
        <p className="text-muted-foreground text-sm">
          Work with the projects and tools your role can access, right from your
          agent.
        </p>
      </div>
      <div className="flex items-center gap-2">
        <Button asChild size="sm" variant="secondary">
          <Link
            to={`${routes.headless.href()}?entrySource=organization_home`}
            onClick={() => {
              telemetry.capture("platform_mcp_member_cta", {
                action: "selected",
                workflow: "organization_home",
              });
            }}
          >
            Connect your agent
          </Link>
        </Button>
        <Button
          size="sm"
          variant="tertiary"
          icon="x"
          aria-label="Dismiss Platform MCP suggestion"
          onClick={() => {
            dismissedStore.write(key, true);
            telemetry.capture("platform_mcp_member_cta", {
              action: "dismissed",
              workflow: "organization_home",
            });
          }}
        />
      </div>
    </div>
  );
}
