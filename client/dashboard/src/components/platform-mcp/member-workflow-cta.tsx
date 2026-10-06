import { useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Dialog } from "@/components/ui/Dialog";
import { PlatformMCPOnboardingContent } from "@/pages/org/PlatformMCP";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import { useOrganization } from "@/contexts/Auth";
import { useOrganizationPlatformMCPOnboarding } from "@/hooks/useOrganizationPlatformMCPOnboarding";
import { useRBAC } from "@/hooks/useRBAC";
import { useTelemetry } from "@/contexts/Telemetry";

type MemberWorkflowCTAProps = {
  label: string;
  prompt: string;
  description: string;
  scope: Scope;
  resourceId: string;
  projectSlug: string;
  workflow:
    | "mcp_diagnostics"
    | "skill_feedback"
    | "skill_create"
    | "skill_improve";
};

export function MemberWorkflowCTA({
  label,
  prompt,
  description,
  scope,
  resourceId,
  projectSlug,
  workflow,
}: MemberWorkflowCTAProps): JSX.Element | null {
  const organization = useOrganization();
  const telemetry = useTelemetry();
  const recordedImpression = useRef<string | null>(null);
  const { hasScope, isLoading, error } = useRBAC();
  const allowed =
    !isLoading &&
    !error &&
    !hasScope("org:admin") &&
    hasScope(scope, resourceId);
  const onboarding = useOrganizationPlatformMCPOnboarding(organization.id, {
    enabled: allowed,
    throwOnError: false,
    staleTime: 10_000,
  });
  const [setupOpen, setSetupOpen] = useState(false);
  const [promptOpen, setPromptOpen] = useState(false);
  const state = onboarding.data;
  const visible = allowed && state?.enabled && !onboarding.isError;
  const impressionKey = `${organization.id}:${resourceId}`;
  useEffect(() => {
    if (!visible || recordedImpression.current === impressionKey) return;
    recordedImpression.current = impressionKey;
    telemetry.capture("platform_mcp_member_cta", {
      action: "impression",
      workflow,
    });
  }, [visible, impressionKey, telemetry, workflow]);

  if (!visible) return null;

  const connected =
    state.connectionAuthorized && state.connectionAuthState === "active";
  const handleClick = () => {
    telemetry.capture("platform_mcp_member_cta", {
      action: "selected",
      workflow,
    });
    if (connected) setPromptOpen(true);
    else setSetupOpen(true);
  };

  return (
    <>
      <div className="border-border bg-card flex flex-wrap items-center justify-between gap-3 border p-4">
        <p className="text-muted-foreground text-sm">{description}</p>
        <Button size="sm" variant="secondary" onClick={handleClick}>
          {connected
            ? label
            : state.connectionAuthState === "reauthorization_required"
              ? "Reconnect your agent"
              : "Connect your agent"}
        </Button>
      </div>
      <PlatformMCPOnboardingContent
        sheetOnly
        setupOpen={setupOpen}
        onSetupOpenChange={setSetupOpen}
        currentProjectSlug={projectSlug}
        onSetupComplete={() => {
          void onboarding.refetch();
          setPromptOpen(true);
        }}
      />
      <Dialog open={promptOpen} onOpenChange={setPromptOpen}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>{label}</Dialog.Title>
            <Dialog.Description>
              Copy this prompt into the agent where you connected Platform MCP.
              Review its suggestions before making changes.
            </Dialog.Description>
          </Dialog.Header>
          <div className="border-border bg-card flex items-start gap-3 border p-4">
            <p className="min-w-0 flex-1 whitespace-pre-wrap text-sm">
              {prompt}
            </p>
            <CopyButton text={prompt} tooltip="Copy prompt" size="sm" />
          </div>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
