import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useOrganization, useUser } from "@/contexts/Auth";

import type { RecordDashboardCtaEventRequestBody } from "@gram/client/models/components/recorddashboardctaeventrequestbody.js";
import { createPersistedFlagStore } from "@/hooks/usePersistedFlagStore";
import { useOrganizationPlatformMCPOnboarding } from "@/hooks/useOrganizationPlatformMCPOnboarding";
import { useOrgRoutes } from "@/routes";
import { usePlatformMCPPackageStatus } from "@gram/client/react-query/platformMCPPackageStatus.js";
import { usePlatformMcpDashboardVisibility } from "@/hooks/usePlatformMcpDashboardVisibility";
import { useRBAC } from "@/hooks/useRBAC";
import { useRecordPlatformMCPDashboardCtaEventMutation } from "@gram/client/react-query/recordPlatformMCPDashboardCtaEvent.js";

const PLATFORM_MCP_CTA_SURFACES = [
  "sidebar_footer",
  "sources_empty",
  "project_overview_zero_data",
  "organization_home",
] as const;

export type PlatformMcpCtaSurface = (typeof PLATFORM_MCP_CTA_SURFACES)[number];

type PlatformMcpCtaAction = RecordDashboardCtaEventRequestBody["action"];

type PlatformMcpCtaLabel =
  | "Set up Platform MCP"
  | "Continue Platform MCP setup"
  | "Reconnect Platform MCP";

const store = createPersistedFlagStore(
  "gram:platform-mcp-promotion-dismissed:v1",
);

function dismissalScope(
  userID: string,
  organizationID: string,
): string | undefined {
  if (!userID || !organizationID) return undefined;
  return `${userID}:${organizationID}`;
}

export function platformMcpCtaLabel({
  connectionAuthState,
  workflowActive,
  stage,
}: {
  connectionAuthState: string;
  workflowActive: boolean;
  stage: string;
}): PlatformMcpCtaLabel {
  if (connectionAuthState === "reauthorization_required") {
    return "Reconnect Platform MCP";
  }
  if (
    workflowActive ||
    connectionAuthState === "active" ||
    stage !== "not_started"
  ) {
    return "Continue Platform MCP setup";
  }
  return "Set up Platform MCP";
}

/**
 * Server-backed Platform MCP promotion state shared by every dashboard surface.
 * Dismissal is local presentation state; eligibility and lifecycle are server
 * facts and therefore fail closed while either request is unsettled.
 */
export function usePlatformMcpCta({
  surface,
  projectSlug,
}: {
  surface: PlatformMcpCtaSurface;
  projectSlug?: string;
}): {
  visible: boolean;
  label: PlatformMcpCtaLabel;
  href: string;
  dismiss: () => void;
  recordImpression: () => void;
  recordSelected: () => void;
} {
  const orgRoutes = useOrgRoutes();
  const user = useUser();
  const organization = useOrganization();
  const { hasScope } = useRBAC();
  const { enabled: dashboardEnabled, isLoading: dashboardLoading } =
    usePlatformMcpDashboardVisibility();
  const canQuery = dashboardEnabled && hasScope("org:admin");
  const onboarding = useOrganizationPlatformMCPOnboarding(organization.id, {
    throwOnError: false,
    staleTime: 10_000,
    enabled: canQuery,
  });
  const packageStatus = usePlatformMCPPackageStatus(undefined, undefined, {
    throwOnError: false,
    staleTime: 10_000,
    enabled: canQuery,
  });
  const scope = dismissalScope(user.id, organization.id);
  const dismissed = store.useFlag(scope);
  const { mutate: recordDashboardCtaEvent } =
    useRecordPlatformMCPDashboardCtaEventMutation();
  const recorded = useRef(new Set<PlatformMcpCtaAction>());

  const label = platformMcpCtaLabel({
    connectionAuthState:
      onboarding.data?.connectionAuthState ?? "not_connected",
    workflowActive: onboarding.data?.workflowActive ?? false,
    stage: onboarding.data?.stage ?? "not_started",
  });

  const href = useMemo(() => {
    const params = new URLSearchParams({ setup: "1", entrySource: surface });
    if (projectSlug) params.set("projectSlug", projectSlug);
    return `${orgRoutes.platformMcp.href()}?${params.toString()}`;
  }, [orgRoutes.platformMcp, projectSlug, surface]);

  const record = useCallback(
    (action: PlatformMcpCtaAction) => {
      if (recorded.current.has(action)) return;
      recorded.current.add(action);
      recordDashboardCtaEvent({
        security: { sessionHeaderGramSession: "" },
        request: {
          gramSession: "",
          recordDashboardCtaEventRequestBody: { action, surface },
        },
      });
    },
    [recordDashboardCtaEvent, surface],
  );

  const recordImpression = useCallback(() => record("impression"), [record]);
  const recordSelected = useCallback(() => record("selected"), [record]);

  const dismiss = useCallback(() => {
    if (!scope) return;
    store.write(scope, true);
    record("dismissed");
  }, [record, scope]);

  useEffect(() => {
    recorded.current.clear();
  }, [surface, scope]);

  const requiresOrganizationSetup =
    surface === "sidebar_footer" || surface === "organization_home";
  const canRender =
    !dashboardLoading &&
    canQuery &&
    !onboarding.isLoading &&
    !onboarding.isError &&
    onboarding.data?.enabled === true &&
    (!requiresOrganizationSetup ||
      onboarding.data.organizationSetupComplete === false) &&
    !packageStatus.isLoading &&
    !packageStatus.isError &&
    packageStatus.data?.admission === "enabled" &&
    packageStatus.data.available === true &&
    !dismissed;

  return {
    visible: canRender,
    label,
    href,
    dismiss,
    recordImpression,
    recordSelected,
  };
}

export function usePlatformMcpCtaImpression(
  visible: boolean,
  recordImpression: () => void,
): (node: HTMLElement | null) => void {
  const observer = useRef<IntersectionObserver | null>(null);
  const [node, setNode] = useState<HTMLElement | null>(null);

  useEffect(() => {
    observer.current?.disconnect();
    if (!visible || !node) return;
    if (typeof IntersectionObserver === "undefined") {
      recordImpression();
      return;
    }
    observer.current = new IntersectionObserver(
      ([entry]) => {
        if (!entry?.isIntersecting || entry.intersectionRatio < 0.5) return;
        recordImpression();
        observer.current?.disconnect();
      },
      { threshold: 0.5 },
    );
    observer.current.observe(node);
    return () => observer.current?.disconnect();
  }, [node, recordImpression, visible]);

  return setNode;
}
