import {
  Alert,
  AlertDescription,
  AlertTitle,
  ErrorAlert,
} from "@/components/ui/Alert";
import { useOrganization } from "@/contexts/Auth";
import { ArrowLeft, ChevronRight, CircleCheck } from "lucide-react";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { invalidatePlatformMCPOnboarding } from "@gram/client/react-query/platformMCPOnboarding.js";
import { useEffect, useRef, useState } from "react";
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";

import { AgentPlatformPickerItem } from "@/pages/setup/components/agent-platform-picker-item";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import type { ClientFamily } from "@gram/client/models/components/recordinstallintentrequestbody.js";
import { CopyButton } from "@/components/ui/CopyButton";
import { Dialog } from "@/components/ui/Dialog";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { Navigate, useSearchParams } from "react-router";
import { Page } from "@/components/page-layout";
import {
  PlatformMCPInstallWalkthrough,
  type PlatformMCPInstallMethod,
} from "./platform-mcp-install-walkthrough";
import type { PlatformMCPOnboardingState } from "@gram/client/models/components/platformmcponboardingstate.js";
import { RequireScope } from "@/components/require-scope";
import { Skeleton } from "@/components/ui/Skeleton";
import { Spinner } from "@/components/ui/Spinner";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { invalidateAllProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useDismissPlatformMCPOnboardingMutation } from "@gram/client/react-query/dismissPlatformMCPOnboarding.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet.js";
import { useFetcher } from "@/contexts/Fetcher";
import { usePlatformMcpDashboardVisibility } from "@/hooks/usePlatformMcpDashboardVisibility";
import { useOrganizationPlatformMCPOnboarding } from "@/hooks/useOrganizationPlatformMCPOnboarding";
import { useQueryClient } from "@tanstack/react-query";
import { useRecordPlatformMCPAgentConfigurationCopiedMutation } from "@gram/client/react-query/recordPlatformMCPAgentConfigurationCopied.js";
import { useRecordPlatformMCPInstallIntentMutation } from "@gram/client/react-query/recordPlatformMCPInstallIntent.js";
import { useStartPlatformMCPOnboardingMutation } from "@gram/client/react-query/startPlatformMCPOnboarding.js";
import {
  SourceSurface,
  type SourceSurface as SourceSurfaceValue,
} from "@gram/client/models/components/startonboardingrequestbody.js";

const clients: Array<{
  id: ClientFamily;
  label: string;
  description: string;
}> = [
  {
    id: "claude_code",
    label: "Claude Code",
    description: "Anthropic CLI & IDE agent",
  },
  {
    id: "claude_cowork",
    label: "Claude Cowork",
    description: "Claude's autonomous desktop assistant",
  },
  {
    id: "codex",
    label: "OpenAI Codex",
    description: "Codex CLI & Codex mode in the ChatGPT app",
  },
  { id: "cursor", label: "Cursor", description: "AI-powered code editor" },
  {
    id: "opencode",
    label: "opencode",
    description: "Open-source terminal coding agent",
  },
  {
    id: "other",
    label: "Other agent",
    description: "Any MCP-capable agent",
  },
];

function starterPrompt(currentProjectSlug?: string): string {
  if (currentProjectSlug) {
    return `Help me add a reviewed MCP server from the catalogue to the project currently being set up: ${currentProjectSlug}. Show the available catalogue options, then ask me to choose one. Inspect the chosen server and collect only its declared non-secret configuration, including declared URL values where applicable. Register it privately, send me to the secure dashboard setup when needed, verify it is ready, and add it to this project's existing Default plugin. Do not ask me to paste API keys, tokens, passwords, OAuth codes, client secrets, or secret headers into chat. Do not ask me for the MCP server endpoint itself; use the reviewed catalogue entry selected for this project.`;
  }

  return "Help me add a reviewed MCP server to a project. Show the available catalogue options and eligible projects, then ask me to choose one of each. Inspect the chosen server and collect only its declared non-secret configuration, including declared URL values where applicable. Register it privately, send me to the secure dashboard setup when needed, verify it is ready, and add it to that project's existing Default plugin. Do not ask me to paste API keys, tokens, passwords, OAuth codes, client secrets, or secret headers into chat. Do not ask me for the MCP server endpoint itself; use the reviewed catalogue entry selected for this project.";
}

function platformMcpEntrySource(
  value: string | null,
): SourceSurfaceValue | undefined {
  return Object.values(SourceSurface).find((surface) => surface === value);
}

export default function PlatformMCP(): JSX.Element | null {
  const { enabled: platformMcpDashboardEnabled, isLoading } =
    usePlatformMcpDashboardVisibility();
  const [searchParams] = useSearchParams();
  const sourceSurface = platformMcpEntrySource(searchParams.get("entrySource"));
  const currentProjectSlug = searchParams.get("projectSlug") ?? undefined;
  const openFromCta = searchParams.get("setup") === "1" && !!sourceSurface;

  // Wait for rollout flags before routing so an eligible organization never
  // flashes away from a direct dashboard link.
  if (isLoading) {
    return null;
  }
  if (!platformMcpDashboardEnabled) {
    return <Navigate to=".." replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="org:admin" level="page">
          <PlatformMCPOnboardingContent
            currentProjectSlug={currentProjectSlug}
            initialSourceSurface={sourceSurface}
            autoOpen={openFromCta}
          />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

export function PlatformMCPOnboardingContent({
  currentProjectSlug,
  embeddedInProjectSetup = false,
  onSetupComplete,
  sheetOnly = false,
  setupOpen = false,
  onSetupOpenChange,
  initialSourceSurface,
  autoOpen = false,
}: {
  currentProjectSlug?: string;
  embeddedInProjectSetup?: boolean;
  onSetupComplete?: () => void;
  sheetOnly?: boolean;
  setupOpen?: boolean;
  onSetupOpenChange?: (open: boolean) => void;
  initialSourceSurface?: SourceSurfaceValue;
  autoOpen?: boolean;
} = {}): JSX.Element {
  const organization = useOrganization();
  const isCurrentOrganization = useIsCurrentOrganization(organization.id);
  return (
    <PlatformMCPOnboardingContentInner
      key={organization.id}
      organizationId={organization.id}
      isCurrentOrganization={isCurrentOrganization}
      currentProjectSlug={currentProjectSlug}
      embeddedInProjectSetup={embeddedInProjectSetup}
      onSetupComplete={onSetupComplete}
      sheetOnly={sheetOnly}
      setupOpen={setupOpen}
      onSetupOpenChange={onSetupOpenChange}
      initialSourceSurface={initialSourceSurface}
      autoOpen={autoOpen}
    />
  );
}

function PlatformMCPOnboardingContentInner({
  organizationId,
  isCurrentOrganization,
  currentProjectSlug,
  embeddedInProjectSetup = false,
  onSetupComplete,
  sheetOnly = false,
  setupOpen = false,
  onSetupOpenChange,
  initialSourceSurface,
  autoOpen = false,
}: {
  organizationId: string;
  isCurrentOrganization: () => boolean;
  currentProjectSlug?: string;
  embeddedInProjectSetup?: boolean;
  onSetupComplete?: () => void;
  sheetOnly?: boolean;
  setupOpen?: boolean;
  onSetupOpenChange?: (open: boolean) => void;
  initialSourceSurface?: SourceSurfaceValue;
  autoOpen?: boolean;
}): JSX.Element {
  const queryClient = useQueryClient();
  const { fetch: authedFetch } = useFetcher();
  const [setupError, setSetupError] = useState<string | null>(null);
  const [accessError, setAccessError] = useState<string | null>(null);
  const [disableConfirmationOpen, setDisableConfirmationOpen] = useState(false);
  const [agentPickerOpen, setAgentPickerOpen] = useState(false);
  const [installMethodPickerOpen, setInstallMethodPickerOpen] = useState(false);
  const [selectedClientID, setSelectedClientID] = useState<ClientFamily | null>(
    null,
  );
  const [selectedInstallMethod, setSelectedInstallMethod] =
    useState<PlatformMCPInstallMethod>("marketplace");
  const [setupSheetOpen, setSetupSheetOpen] = useState(false);
  const [sourceSurface] = useState<SourceSurfaceValue>(
    initialSourceSurface ?? SourceSurface.PlatformMcpSettings,
  );
  const [searchParams, setSearchParams] = useSearchParams();
  const onboarding = useOrganizationPlatformMCPOnboarding(organizationId, {
    throwOnError: false,
    staleTime: 10_000,
    enabled: !sheetOnly || setupOpen,
    refetchInterval: setupOpen || !sheetOnly ? 5_000 : false,
    refetchIntervalInBackground: false,
  });

  useEffect(() => {
    if (!sheetOnly) return;
    if (setupOpen) {
      setAgentPickerOpen(true);
      setInstallMethodPickerOpen(false);
      setSetupSheetOpen(false);
    } else {
      setAgentPickerOpen(false);
      setInstallMethodPickerOpen(false);
      setSetupSheetOpen(false);
    }
  }, [setupOpen, sheetOnly]);

  const closeSetupFlow = () => {
    setAgentPickerOpen(false);
    setInstallMethodPickerOpen(false);
    setSetupSheetOpen(false);
    onSetupOpenChange?.(false);
  };

  const openSetupFlow = () => {
    setAgentPickerOpen(true);
    setInstallMethodPickerOpen(false);
    setSetupSheetOpen(false);
    onSetupOpenChange?.(true);
  };

  useEffect(() => {
    if (!autoOpen || sheetOnly) return;
    setAgentPickerOpen(true);
    setInstallMethodPickerOpen(false);
    setSetupSheetOpen(false);
    onSetupOpenChange?.(true);
    const next = new URLSearchParams(searchParams);
    next.delete("setup");
    next.delete("entrySource");
    setSearchParams(next, { replace: true });
  }, [autoOpen, onSetupOpenChange, searchParams, setSearchParams, sheetOnly]);

  const invalidate = async () => {
    await invalidatePlatformMCPOnboarding(queryClient, [{ gramSession: "" }], {
      refetchType: "active",
    });
  };

  const start = useStartPlatformMCPOnboardingMutation({
    onSuccess: () => void invalidate(),
  });
  const selectClient = useRecordPlatformMCPInstallIntentMutation({
    onSuccess: () => void invalidate(),
  });
  const recordConfigurationCopied =
    useRecordPlatformMCPAgentConfigurationCopiedMutation({
      onSuccess: () => void invalidate(),
    });
  const dismiss = useDismissPlatformMCPOnboardingMutation({
    onSuccess: () => void invalidate(),
  });
  const setOrganizationAccess = useFeaturesSetMutation({
    onSuccess: async () => {
      if (!isCurrentOrganization()) return;
      setAccessError(null);
      setDisableConfirmationOpen(false);
      await Promise.all([
        invalidateAllProductFeatures(queryClient),
        invalidate(),
      ]);
    },
    onError: () => {
      if (!isCurrentOrganization()) return;
      setAccessError(
        "Could not update organization-wide Platform MCP access. Try again.",
      );
    },
  });

  const setPlatformMCPAccess = (enabled: boolean) => {
    setAccessError(null);
    setOrganizationAccess.mutate({
      request: {
        setProductFeatureRequestBody: {
          organizationId,
          featureName: FeatureName.PlatformMcp,
          enabled,
        },
      },
    });
  };

  if (sheetOnly && !setupOpen) {
    return <></>;
  }

  if (onboarding.isLoading) {
    return sheetOnly ? (
      <PlatformMCPStateSheet
        open={setupOpen}
        onOpenChange={(open) => {
          if (!open) onSetupOpenChange?.(false);
        }}
        eyebrow="Platform MCP"
        title="Loading setup"
        description="Loading your organization’s current setup progress."
      >
        <PlatformMCPLoading />
      </PlatformMCPStateSheet>
    ) : (
      <PlatformMCPLoading />
    );
  }

  if (onboarding.error || !onboarding.data) {
    const unavailable = (
      <div className="flex flex-col gap-3">
        <ErrorAlert
          title="Platform MCP is unavailable"
          error="Refresh the page or try again."
        />
        <Button
          variant="secondary"
          className="self-start"
          onClick={() => void onboarding.refetch()}
        >
          Try again
        </Button>
      </div>
    );
    return sheetOnly ? (
      <PlatformMCPStateSheet
        open={setupOpen}
        onOpenChange={(open) => {
          if (!open) onSetupOpenChange?.(false);
        }}
        eyebrow="Platform MCP"
        title="Setup unavailable"
        description="The setup state could not be loaded."
      >
        {unavailable}
      </PlatformMCPStateSheet>
    ) : (
      <div className="mx-auto mt-8 max-w-xl">{unavailable}</div>
    );
  }

  const state = onboarding.data;
  if (!state.enabled) {
    const unavailable = (
      <PlatformMCPUnavailable
        state={state}
        isMutating={setOrganizationAccess.isPending}
        accessError={accessError}
        onEnable={() => setPlatformMCPAccess(true)}
      />
    );
    return sheetOnly ? (
      <PlatformMCPStateSheet
        open={setupOpen}
        onOpenChange={(open) => {
          if (!open) onSetupOpenChange?.(false);
        }}
        eyebrow="Organization access"
        title="Turn on Platform MCP"
        description="Existing connections and project distributions are kept — they stay unavailable until an organization administrator enables access."
      >
        {unavailable}
      </PlatformMCPStateSheet>
    ) : (
      unavailable
    );
  }

  const activeClient =
    clients.find(
      (client) => client.id === (selectedClientID ?? state.clientFamily),
    ) ?? clients[0]!;
  // The final checklist item is the durable existing-Default-plugin attachment.
  // Earlier evidence, including an authenticated connection, is setup progress
  // rather than completion and must not unlock organization management.
  const setupComplete = state.distributionAttached;
  const reconnectRequired =
    state.connectionAuthState === "reauthorization_required";
  // The project wizard remains an onboarding surface even if this organization
  // already completed Platform MCP setup elsewhere. Management belongs only on
  // the standalone organization route.
  const showManagement = !embeddedInProjectSetup && setupComplete;
  const isMutating =
    start.isPending ||
    selectClient.isPending ||
    recordConfigurationCopied.isPending ||
    dismiss.isPending ||
    setOrganizationAccess.isPending;

  const selectAgentForSetup = (client: PlatformMCPClient) => {
    setSelectedClientID(client.id);

    const recordIntent = () =>
      selectClient.mutate(
        {
          security: { sessionHeaderGramSession: "" },
          request: {
            recordInstallIntentRequestBody: { clientFamily: client.id },
          },
        },
        {
          onSuccess: () => {
            // Wait for the selected client and any fresh-workflow reset to reach
            // the shared query before the next sheet can be opened. Otherwise a
            // fast click can briefly render the previous workflow's evidence.
            void invalidate().then(() => {
              setAgentPickerOpen(false);
              setInstallMethodPickerOpen(true);
            });
          },
        },
      );
    const startWorkflow = () =>
      start.mutate(
        {
          security: { sessionHeaderGramSession: "" },
          request: {
            startOnboardingRequestBody: { sourceSurface },
          },
        },
        { onSuccess: recordIntent },
      );

    if (state.workflowActive && setupComplete) {
      // A completed workflow retains its evidence until explicitly closed. Close
      // it before "set up another agent" so catalogue, registration, readiness,
      // and distribution are tracked against a genuinely fresh workflow.
      dismiss.mutate(
        { security: { sessionHeaderGramSession: "" } },
        { onSuccess: startWorkflow },
      );
      return;
    }
    if (state.workflowActive) {
      recordIntent();
      return;
    }
    startWorkflow();
  };

  const selectInstallMethod = (method: PlatformMCPInstallMethod) => {
    setSelectedInstallMethod(method);
    setInstallMethodPickerOpen(false);
    setSetupSheetOpen(true);
  };

  const continueSecureSetup = async () => {
    setSetupError(null);
    try {
      // SDK generation is blocked by local certificate trust, so this uses the
      // dashboard's authenticated request helper while the generated response
      // model still has the old handoff-only contract. No provider material is
      // sent by this request.
      const continuation = await authedFetch(
        "/rpc/platformMcp.startOnboardingSetup",
        { method: "POST" },
      );
      if (!continuation.ok) {
        throw new Error("secure setup continuation failed");
      }
      const result = (await continuation.json()) as {
        handoff?: string;
        dashboard_setup_url?: string;
      };
      if (result.dashboard_setup_url) {
        window.location.assign(result.dashboard_setup_url);
        return;
      }
      if (!result.handoff) {
        throw new Error("secure setup continuation missing");
      }
      const response = await authedFetch("/platform-mcp/provider-setup", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ handoff: result.handoff }),
      });
      if (!response.ok) {
        throw new Error("secure setup request failed");
      }
      const setup = (await response.json()) as { authorization_url?: string };
      if (!setup.authorization_url) {
        throw new Error("secure setup URL missing");
      }
      window.location.assign(setup.authorization_url);
    } catch {
      setSetupError("Could not start secure setup. Try again.");
    }
  };

  const setupSheets = (
    <>
      <PlatformMCPAgentPickerSheet
        open={agentPickerOpen}
        onOpenChange={(open) => {
          if (!open) closeSetupFlow();
        }}
        isMutating={isMutating}
        onSelect={selectAgentForSetup}
      />

      <PlatformMCPInstallMethodSheet
        open={installMethodPickerOpen}
        onOpenChange={(open) => {
          if (!open) closeSetupFlow();
        }}
        client={activeClient}
        onBack={() => {
          setInstallMethodPickerOpen(false);
          setAgentPickerOpen(true);
        }}
        onSelect={selectInstallMethod}
      />

      <PlatformMCPSetupSheet
        open={setupSheetOpen}
        onOpenChange={(open) => {
          setSetupSheetOpen(open);
          if (!open) closeSetupFlow();
        }}
        state={state}
        currentProjectSlug={currentProjectSlug}
        activeClient={activeClient}
        installMethod={selectedInstallMethod}
        isMutating={isMutating}
        setupError={setupError}
        onBackToInstallMethod={() => {
          setSetupSheetOpen(false);
          setInstallMethodPickerOpen(true);
        }}
        onConfigurationCopied={() => {
          setSetupSheetOpen(true);
          recordConfigurationCopied.mutate({
            security: { sessionHeaderGramSession: "" },
          });
        }}
        onContinueSecureSetup={() => void continueSecureSetup()}
        onDismiss={() =>
          dismiss.mutate(
            { security: { sessionHeaderGramSession: "" } },
            { onSuccess: closeSetupFlow },
          )
        }
        onDone={() => {
          closeSetupFlow();
          onSetupComplete?.();
        }}
      />
    </>
  );

  if (sheetOnly) {
    return <>{setupSheets}</>;
  }

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-6">
      <Page.Section>
        <Page.Section.Title stage="preview">Platform MCP</Page.Section.Title>
        <Page.Section.Description className="max-w-3xl">
          Manage MCPs, Risk Policies and explore logs in your favorite agent.
        </Page.Section.Description>
        {showManagement ? (
          <Page.Section.Body>
            <Text variant="subheading" className="mb-3">
              Manage Platform MCP
            </Text>
            <PlatformMCPManagement
              state={state}
              isMutating={setOrganizationAccess.isPending}
              accessError={accessError}
              onDisable={() => setDisableConfirmationOpen(true)}
            />
          </Page.Section.Body>
        ) : null}
      </Page.Section>

      {reconnectRequired ? (
        <PlatformMCPReconnect
          reason={state.reauthorizationReason}
          isMutating={isMutating}
          onReconnect={openSetupFlow}
        />
      ) : null}

      <section
        className="border bg-card p-6"
        aria-labelledby="platform-mcp-setup"
      >
        <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <Text variant="subheading" id="platform-mcp-setup">
              {showManagement ? "Set up another agent" : "Set up Platform MCP"}
            </Text>
            <Text muted small className="mt-2 max-w-2xl">
              {showManagement
                ? "Start a separate resumable checklist for another agent in this organization."
                : "Connect an agent, choose a reviewed MCP server, complete any required setup, and add it to the selected project's existing Default plugin."}
            </Text>
          </div>
          {!agentPickerOpen ? (
            <Button disabled={isMutating} onClick={openSetupFlow}>
              <Button.Text>Start setup</Button.Text>
            </Button>
          ) : null}
        </div>
      </section>

      {setupSheets}

      <Dialog
        open={disableConfirmationOpen}
        onOpenChange={(open) => {
          if (!setOrganizationAccess.isPending) {
            setDisableConfirmationOpen(open);
          }
        }}
      >
        <Dialog.Content closeable={!setOrganizationAccess.isPending}>
          <Dialog.Header>
            <Dialog.Title>
              Disable Platform MCP for this organization?
            </Dialog.Title>
            <Dialog.Description>
              This immediately prevents everyone in this organization from
              connecting to or using Platform MCP. Existing connections and
              project distributions are retained, but they cannot be used until
              an organization administrator enables Platform MCP again.
            </Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Button
              variant="secondary"
              disabled={setOrganizationAccess.isPending}
              onClick={() => setDisableConfirmationOpen(false)}
            >
              Cancel
            </Button>
            <Button
              variant="destructive-primary"
              disabled={setOrganizationAccess.isPending}
              onClick={() => setPlatformMCPAccess(false)}
            >
              <Button.Text>
                {setOrganizationAccess.isPending
                  ? "Disabling…"
                  : "Disable Platform MCP"}
              </Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </div>
  );
}

function PlatformMCPStateSheet({
  open,
  onOpenChange,
  eyebrow,
  title,
  description,
  footer,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  eyebrow: string;
  title: string;
  description: string;
  footer?: React.ReactNode;
  children?: React.ReactNode;
}): JSX.Element {
  // Same frame as the setup wizard's instrumentation sheet: the accessible
  // header is visually hidden, the heading block lives in the body as
  // eyebrow → heading → one line of context, and actions sit in a footer bar
  // divided by a hairline.
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col overflow-hidden sm:max-w-[662px]"
      >
        <SheetHeader className="sr-only">
          <SheetTitle>{title}</SheetTitle>
          <SheetDescription>{description}</SheetDescription>
        </SheetHeader>
        <div className="w-full min-w-0 flex-1 space-y-4 overflow-y-auto px-6 pt-6 pr-14 pb-6">
          <div>
            <p className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
              {eyebrow}
            </p>
            <h3 className="text-foreground mt-1 text-lg font-semibold">
              {title}
            </h3>
            <p className="text-muted-foreground mt-1 text-sm">{description}</p>
          </div>
          {children}
        </div>
        {footer && (
          <div className="border-border flex items-center justify-end border-t px-6 py-4">
            {footer}
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}

function PlatformMCPUnavailable({
  state,
  isMutating,
  accessError,
  onEnable,
}: {
  state: PlatformMCPOnboardingState;
  isMutating: boolean;
  accessError: string | null;
  onEnable: () => void;
}): JSX.Element {
  return (
    <div className="space-y-4">
      {state.repairAction === "enable_platform_mcp" && (
        <div className="border-border bg-card flex flex-col gap-3 border p-4">
          <Text muted small>
            Enabling lets organization administrators connect agents and use the
            Platform MCP workflow again. No connection is created or restored
            automatically.
          </Text>
          <Button
            className="self-start"
            disabled={isMutating}
            onClick={onEnable}
          >
            <Button.Text>
              {isMutating ? "Enabling…" : "Enable Platform MCP"}
            </Button.Text>
          </Button>
        </div>
      )}
      {accessError && (
        <ErrorAlert title="Could not enable Platform MCP" error={accessError} />
      )}
    </div>
  );
}

function PlatformMCPReconnect({
  reason,
  isMutating,
  onReconnect,
}: {
  reason:
    | ""
    | "idle_expired"
    | "authorization_expired"
    | "refresh_invalidated"
    | "authorization_changed"
    | "revoked"
    | "security_reset";
  isMutating: boolean;
  onReconnect: () => void;
}): JSX.Element {
  const messages = {
    idle_expired:
      "This connection was not refreshed for 30 days. Reconnect to start a new authorization period.",
    authorization_expired:
      "This connection reached its 90-day authorization limit. Reconnect to continue using Platform MCP.",
    refresh_invalidated:
      "This connection was reset because a refresh credential could not be safely accepted. Reconnect to continue.",
    authorization_changed:
      "Your current organization authorization no longer matches this connection. Reconnect after confirming your access.",
    revoked:
      "This connection or its OAuth client was revoked. Reconnect with a supported client to continue.",
    security_reset:
      "This connection was reset for security. Reconnect before using Platform MCP again.",
    "": "This connection can no longer refresh silently. Reconnect to continue using Platform MCP.",
  } as const;

  return (
    <Alert variant="warning">
      <div className="flex flex-1 flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <AlertTitle>Reconnect Platform MCP</AlertTitle>
          <AlertDescription>{messages[reason]}</AlertDescription>
        </div>
        <Button
          className="shrink-0 self-start"
          disabled={isMutating}
          onClick={onReconnect}
        >
          <Button.Text>Reconnect Platform MCP</Button.Text>
        </Button>
      </div>
    </Alert>
  );
}

function PlatformMCPManagement({
  state,
  isMutating,
  accessError,
  onDisable,
}: {
  state: PlatformMCPOnboardingState;
  isMutating: boolean;
  accessError: string | null;
  onDisable: () => void;
}): JSX.Element {
  const reconnectRequired =
    "connectionAuthState" in state &&
    state.connectionAuthState === "reauthorization_required";
  const connectionStatus = reconnectRequired
    ? "Reconnect required"
    : state.connectionReady
      ? "Ready"
      : state.connectionAuthorized
        ? "Authorized"
        : "Not connected";
  const connectionVariant = reconnectRequired
    ? "warning"
    : state.connectionReady
      ? "success"
      : state.connectionAuthorized
        ? "information"
        : "warning";
  const selectedProject =
    state.selectedProjectName || state.selectedProjectSlug;

  return (
    <div className="mb-6 grid gap-4 rounded-xl border bg-card p-5 lg:grid-cols-2">
      <div className="space-y-4">
        <div className="flex flex-wrap items-center gap-2">
          <Text variant="subheading">Organization access</Text>
          <Badge variant="success" size="sm">
            Enabled
          </Badge>
        </div>
        <Text muted small className="max-w-xl">
          Platform MCP is available to organization administrators. Disabling it
          stops every person in this organization from connecting to or using
          Platform MCP; it does not delete existing connections or project
          distributions.
        </Text>
        <Button
          variant="destructive-secondary"
          className="self-start"
          disabled={isMutating}
          onClick={onDisable}
        >
          <Button.Text>Disable organization access</Button.Text>
        </Button>
        {accessError && (
          <ErrorAlert
            title="Could not update Platform MCP access"
            error={accessError}
          />
        )}
      </div>

      <div className="space-y-4 border-t pt-4 lg:border-t-0 lg:border-l lg:pt-0 lg:pl-5">
        <div>
          <Text variant="subheading">Current setup status</Text>
          <Text muted small className="mt-1">
            This reflects your onboarding connection and selected project. An
            organization-wide connection inventory is not available on this page
            yet.
          </Text>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <ManagementStatus label="Your connection" variant={connectionVariant}>
            {connectionStatus}
          </ManagementStatus>
          <ManagementStatus
            label="Selected project"
            variant={state.distributionAttached ? "success" : "neutral"}
          >
            {selectedProject
              ? state.distributionAttached
                ? `${selectedProject} attached`
                : `${selectedProject} not attached`
              : "No project selected"}
          </ManagementStatus>
        </div>
        <div>
          <Text small className="font-medium">
            Core Platform MCP tools
          </Text>
          <div className="mt-2 flex flex-wrap gap-1.5">
            {[
              "get_platform_context",
              "list_projects",
              "search_mcp_catalog",
              "inspect_mcp_candidate",
              "register_platform_mcp_for_project",
              "get_platform_mcp_onboarding_status",
              "attach_platform_mcp_identity_provider",
              "distribute_mcp_to_plugin",
            ].map((tool) => (
              <code
                key={tool}
                className="rounded border bg-muted/30 px-1.5 py-0.5 text-xs"
              >
                {tool}
              </code>
            ))}
          </div>
          <Text muted small className="mt-2">
            These tools discover reviewed options, guide secure setup and
            readiness, and add a ready MCP to one exact existing plugin in the
            chosen project.
          </Text>
        </div>
      </div>
    </div>
  );
}

function ManagementStatus({
  label,
  variant,
  children,
}: {
  label: string;
  variant: "neutral" | "information" | "success" | "warning";
  children: string;
}): JSX.Element {
  return (
    <div className="rounded-lg border p-3">
      <Text muted small>
        {label}
      </Text>
      <Badge variant={variant} size="sm" className="mt-1">
        {children}
      </Badge>
    </div>
  );
}

function CopyValue({
  label,
  value,
  codeBlock = false,
  onCopy,
}: {
  label: string;
  value: string;
  codeBlock?: boolean;
  onCopy?: () => void;
}): JSX.Element {
  return (
    <div>
      <Text small muted className="mb-1">
        {label}
      </Text>
      <div
        className={
          codeBlock
            ? "border-border bg-muted/30 relative rounded-md border p-3"
            : "border-border bg-muted/30 flex items-center gap-2 rounded-md border px-3 py-2"
        }
      >
        <code
          className={
            codeBlock
              ? "block overflow-x-auto whitespace-pre text-xs"
              : "min-w-0 flex-1 break-all text-xs"
          }
        >
          {value}
        </code>
        <CopyButton
          text={value}
          size="xs"
          tooltip={`Copy ${label.toLowerCase()}`}
          className={codeBlock ? "absolute top-2 right-2" : undefined}
          onCopy={onCopy}
        />
      </div>
    </div>
  );
}

type PlatformMCPClient = (typeof clients)[number];

const SETUP_PRIMER_STEP_COUNT = 2;
const SETUP_LIFECYCLE_STEP_COUNT = 5;
const SETUP_TOTAL_STEP_COUNT =
  SETUP_PRIMER_STEP_COUNT + SETUP_LIFECYCLE_STEP_COUNT;

function PlatformMCPProgress({ step }: { step: number }): JSX.Element {
  return (
    <div className="flex items-center gap-1.5 px-6 pt-6 pr-14">
      {Array.from({ length: SETUP_TOTAL_STEP_COUNT }, (_, index) => (
        <span
          key={index}
          className={cn(
            "h-1 rounded-full transition-all",
            index === step - 1
              ? "bg-foreground w-6"
              : index < step - 1
                ? "bg-foreground/40 w-4"
                : "bg-border w-4",
          )}
        />
      ))}
      <span className="text-muted-foreground ml-auto text-[11px] tabular-nums">
        {step}/{SETUP_TOTAL_STEP_COUNT}
      </span>
    </div>
  );
}

function PlatformMCPAgentPickerSheet({
  open,
  onOpenChange,
  isMutating,
  onSelect,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  isMutating: boolean;
  onSelect: (client: PlatformMCPClient) => void;
}): JSX.Element {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col overflow-hidden sm:max-w-[662px]"
      >
        <SheetHeader className="sr-only">
          <SheetTitle>Choose an agent</SheetTitle>
          <SheetDescription>
            Choose the coding agent where you want to install Platform MCP.
          </SheetDescription>
        </SheetHeader>
        <PlatformMCPProgress step={1} />
        <div className="flex-1 overflow-y-auto px-6 py-6">
          <p className="text-eyebrow">Step 1</p>
          <h2 className="text-display-xs mt-1 font-thin">Choose an agent</h2>
          <p className="text-muted-foreground mt-2 text-sm">
            Pick the coding agent you&apos;re setting up. The next step will
            show the installation methods available for that agent.
          </p>
          <div className="mt-5 space-y-2">
            {clients.map((client) => (
              <AgentPlatformPickerItem
                key={client.id}
                platformId={client.id.replaceAll("_", "-")}
                name={client.label}
                description={client.description}
                disabled={isMutating}
                onClick={() => onSelect(client)}
              />
            ))}
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
}

function PlatformMCPInstallMethodSheet({
  open,
  onOpenChange,
  client,
  onBack,
  onSelect,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  client: PlatformMCPClient;
  onBack: () => void;
  onSelect: (method: PlatformMCPInstallMethod) => void;
}): JSX.Element {
  // No reviewed plugin package is built for an agent we have not certified, so
  // the marketplace route is closed and only the remote MCP config is offered.
  const supportsPackages = client.id !== "other";
  const methods: Array<{
    id: PlatformMCPInstallMethod;
    title: string;
    description: string;
  }> = supportsPackages
    ? [
        {
          id: "marketplace",
          title: "Install from the Speakeasy marketplace",
          description:
            "Recommended. Install the reviewed plugin and receive future updates from the public GitHub marketplace.",
        },
        {
          id: "manual",
          title: "Connect the MCP manually",
          description:
            "Recovery option. Configure only the remote MCP without the reviewed catalogue workflow skill.",
        },
      ]
    : // The marketplace route is listed only where a reviewed package exists.
      // Offering it greyed out here would read as an organization problem
      // rather than what it is: no plugin is built for an uncertified agent.
      [
        {
          id: "manual",
          title: "Connect the MCP manually",
          description:
            "Configure the remote MCP in your agent's own configuration. The reviewed catalogue workflow skill is not installed.",
        },
      ];

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col overflow-hidden sm:max-w-[662px]"
      >
        <SheetHeader className="sr-only">
          <SheetTitle>Choose an install method</SheetTitle>
          <SheetDescription>
            Choose how to install Platform MCP for {client.label}.
          </SheetDescription>
        </SheetHeader>
        <PlatformMCPProgress step={2} />
        <div className="flex-1 overflow-y-auto px-6 py-6">
          <p className="text-eyebrow">Step 2</p>
          <h2 className="text-display-xs mt-1 font-thin">
            Choose an install method
          </h2>
          <p className="text-muted-foreground mt-2 text-sm">
            Install Platform MCP for your {client.label} account. Installation
            never grants access by itself; you&apos;ll authorize in the
            following step.
          </p>
          <div className="mt-5 space-y-2">
            {methods.map((method) => (
              <button
                key={method.id}
                type="button"
                onClick={() => onSelect(method.id)}
                className="border-border bg-card hover:border-foreground/20 flex w-full items-center gap-4 border p-4 text-left transition-all"
              >
                <div className="min-w-0 flex-1 space-y-1">
                  <p className="text-foreground text-sm font-medium">
                    {method.title}
                  </p>
                  <p className="text-muted-foreground text-xs">
                    {method.description}
                  </p>
                </div>
                <ChevronRight className="text-muted-foreground h-4 w-4 shrink-0" />
              </button>
            ))}
          </div>
        </div>
        <SheetFooter className="border-border border-t px-6 py-4">
          <Button variant="tertiary" onClick={onBack}>
            <Button.LeftIcon>
              <ArrowLeft className="h-3 w-3" />
            </Button.LeftIcon>
            <Button.Text>Back</Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

type PlatformMCPStep = {
  title: string;
  complete: boolean;
};

function PlatformMCPSetupSheet({
  open,
  onOpenChange,
  state,
  currentProjectSlug,
  activeClient,
  installMethod,
  isMutating,
  setupError,
  onBackToInstallMethod,
  onConfigurationCopied,
  onContinueSecureSetup,
  onDismiss,
  onDone,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  state: PlatformMCPOnboardingState;
  currentProjectSlug?: string;
  activeClient: PlatformMCPClient;
  installMethod: PlatformMCPInstallMethod;
  isMutating: boolean;
  setupError: string | null;
  onBackToInstallMethod: () => void;
  onConfigurationCopied: () => void;
  onContinueSecureSetup: () => void;
  onDismiss: () => void;
  onDone: () => void;
}): JSX.Element {
  const steps: PlatformMCPStep[] = [
    { title: "Install and authenticate", complete: state.connectionAuthorized },
    { title: "Explore the MCP Catalogue", complete: state.catalogExplored },
    {
      title: "Register the selected MCP",
      complete: state.registrationComplete,
    },
    { title: "Complete secure setup", complete: state.readinessVerified },
    {
      title: "Add it to the Default plugin",
      complete: state.distributionAttached,
    },
  ];
  const firstIncompleteStepIndex = steps.findIndex((step) => !step.complete);
  const evidenceStepIndex =
    firstIncompleteStepIndex === -1
      ? steps.length - 1
      : firstIncompleteStepIndex;
  const completedStepCount = steps.filter((step) => step.complete).length;
  const [currentStepIndex, setCurrentStepIndex] = useState(evidenceStepIndex);
  const [completionAcknowledgementStep, setCompletionAcknowledgementStep] =
    useState<number | null>(null);
  const wasOpenRef = useRef(false);
  const completedStepCountRef = useRef(completedStepCount);
  const currentStep = steps[currentStepIndex]!;
  const allEvidenceComplete = firstIncompleteStepIndex === -1;
  const isAcknowledgingCompletion = completionAcknowledgementStep !== null;

  useEffect(() => {
    const wasOpen = wasOpenRef.current;
    const previousCompletedStepCount = completedStepCountRef.current;
    wasOpenRef.current = open;
    completedStepCountRef.current = completedStepCount;

    if (!open) {
      setCompletionAcknowledgementStep(null);
      return;
    }

    if (!wasOpen) {
      // Every newly selected agent and install method starts with the actual
      // installation instructions, even when this organization completed an
      // earlier Platform MCP workflow. Existing evidence still marks later
      // steps complete and lets the user advance through them immediately.
      setCurrentStepIndex(0);
      setCompletionAcknowledgementStep(null);
      return;
    }

    if (
      completedStepCount > previousCompletedStepCount &&
      currentStepIndex === previousCompletedStepCount
    ) {
      setCompletionAcknowledgementStep(currentStepIndex);
      const timer = window.setTimeout(() => {
        setCompletionAcknowledgementStep(null);
        setCurrentStepIndex(evidenceStepIndex);
      }, 1_250);
      return () => window.clearTimeout(timer);
    }
  }, [completedStepCount, currentStepIndex, evidenceStepIndex, open]);

  const stepCompleted = (message: string): JSX.Element => (
    <div className="border-success/40 bg-success/5 flex min-h-48 flex-col items-center justify-center gap-3 border px-6 text-center">
      <CircleCheck className="text-success size-7" aria-hidden="true" />
      <Badge variant="success" size="sm">
        Step completed
      </Badge>
      <Text small className="font-medium">
        {currentStep.title} is complete
      </Text>
      <Text muted small className="max-w-sm">
        {message}
      </Text>
    </div>
  );

  const waitingFor = (title: string, message: string): JSX.Element => {
    if (completionAcknowledgementStep === currentStepIndex) {
      return stepCompleted("Moving to the next step…");
    }
    if (currentStep.complete) {
      return stepCompleted("You can review this step or continue when ready.");
    }
    return (
      <div className="border-border bg-muted/30 flex min-h-48 flex-col items-center justify-center gap-3 border px-6 text-center">
        <Spinner className="text-muted-foreground mr-0 size-5" />
        <Text small className="font-medium">
          {title}
        </Text>
        <Text muted small className="max-w-sm">
          {message}
        </Text>
      </div>
    );
  };

  const showStep = (): JSX.Element => {
    switch (currentStepIndex) {
      case 0:
        return (
          <>
            <Text muted small>
              Follow the personalized {activeClient.label} steps below to
              install Platform MCP for your account. Installing or copying
              instructions does not complete this stage; current AI Control
              Plane authorization does.
            </Text>
            <PlatformMCPInstallWalkthrough
              initialClient={activeClient.id}
              initialMethod={installMethod}
              mcpUrl={state.mcpUrl}
              allowMethodSelection={false}
              onInstructionIntent={onConfigurationCopied}
            />
            {waitingFor(
              "Waiting for authentication",
              `We are waiting for ${activeClient.label} to connect to Platform MCP after you finish the browser sign-in.`,
            )}
          </>
        );
      case 1:
        return (
          <>
            <Text muted small>
              Your agent is authenticated. Send it this prompt to explore the
              reviewed MCP Catalogue before choosing anything to set up.
            </Text>
            <CopyValue
              label="Suggested prompt"
              value={starterPrompt(currentProjectSlug)}
            />
            {waitingFor(
              "Waiting for catalogue exploration",
              "Send the guided prompt in your agent. This step completes when the agent successfully searches the reviewed MCP Catalogue.",
            )}
          </>
        );
      case 2:
        return (
          <>
            <Text muted small>
              Choose the reviewed MCP you want to distribute. Your agent will
              register that exact MCP privately for the selected project before
              you continue with secure setup.
            </Text>
            {waitingFor(
              "Waiting for the selected MCP to be registered",
              "After the agent presents the reviewed options, choose the MCP you want to distribute. This step completes when the agent privately registers that exact MCP for the selected project.",
            )}
          </>
        );
      case 3:
        return (
          <>
            <Text muted small>
              Complete any secure setup the selected MCP requires, then let your
              agent confirm it is ready before it can be distributed.
            </Text>
            {waitingFor(
              "Waiting for secure setup and readiness",
              "If secure setup is required, complete the dashboard or provider authorization, then return to your agent. This step completes when the agent's fresh readiness check confirms the MCP is ready.",
            )}
            {state.registrationComplete && !state.readinessVerified && (
              <Alert>
                <div>
                  <AlertTitle>Secure setup may be required</AlertTitle>
                  <AlertDescription>
                    If your agent sends you to an Inspect or authentication
                    page, complete that browser action, then return to the agent
                    so it can check readiness again.
                  </AlertDescription>
                </div>
                <Button
                  className="self-start"
                  disabled={isMutating}
                  onClick={onContinueSecureSetup}
                >
                  <Button.Text>Open secure setup</Button.Text>
                </Button>
              </Alert>
            )}
            {setupError && (
              <ErrorAlert title="Secure setup unavailable" error={setupError} />
            )}
          </>
        );
      default:
        return state.distributionAttached ? (
          <Alert>
            <div>
              <AlertTitle>Setup complete</AlertTitle>
              <AlertDescription>
                Your agent added the ready MCP to the selected project&apos;s
                existing Default plugin.
              </AlertDescription>
            </div>
          </Alert>
        ) : (
          <>
            <Text muted small>
              Your agent can now make the ready MCP available to the selected
              project&apos;s users through its existing Default plugin.
            </Text>
            {waitingFor(
              "Waiting for Default plugin distribution",
              "Your agent needs to add the ready MCP to the selected project's existing Default plugin. This step completes when the live attachment is recorded.",
            )}
          </>
        );
    }
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col overflow-hidden sm:max-w-[662px]"
      >
        <SheetHeader className="sr-only">
          <SheetTitle>Set up Platform MCP</SheetTitle>
          <SheetDescription>
            Complete Platform MCP setup one lifecycle step at a time.
          </SheetDescription>
        </SheetHeader>
        <div className="flex items-center gap-1.5 px-6 pt-6 pr-14">
          {Array.from({ length: SETUP_TOTAL_STEP_COUNT }, (_, index) => {
            const lifecycleIndex = index - SETUP_PRIMER_STEP_COUNT;
            const isCurrent = lifecycleIndex === currentStepIndex;
            const isComplete =
              index < SETUP_PRIMER_STEP_COUNT ||
              (lifecycleIndex >= 0 && steps[lifecycleIndex]?.complete);
            const canNavigate =
              lifecycleIndex >= 0 && lifecycleIndex <= currentStepIndex;

            return (
              <button
                key={index}
                type="button"
                disabled={isAcknowledgingCompletion || !canNavigate}
                onClick={() => setCurrentStepIndex(lifecycleIndex)}
                className={cn(
                  "h-1 rounded-full transition-all",
                  isCurrent
                    ? "bg-foreground w-6"
                    : isComplete || lifecycleIndex < currentStepIndex
                      ? canNavigate
                        ? "bg-foreground/40 hover:bg-foreground/60 w-4 cursor-pointer"
                        : "bg-foreground/40 w-4 cursor-not-allowed"
                      : "bg-border w-4 cursor-not-allowed",
                )}
                aria-label={`Step ${index + 1}${lifecycleIndex >= 0 ? `: ${steps[lifecycleIndex]?.title}` : ""}${isComplete ? ", complete" : ""}`}
              />
            );
          })}
          <span className="text-muted-foreground ml-auto text-[11px] tabular-nums">
            {currentStepIndex + SETUP_PRIMER_STEP_COUNT + 1}/
            {SETUP_TOTAL_STEP_COUNT}
          </span>
        </div>
        <div className="flex-1 overflow-y-auto px-6 py-6">
          <p className="text-eyebrow">
            Step {currentStepIndex + SETUP_PRIMER_STEP_COUNT + 1}
          </p>
          <div className="mt-1 flex flex-wrap items-center gap-2">
            <h2 className="text-display-xs font-thin">{currentStep.title}</h2>
            {currentStep.complete && (
              <Badge variant="success" size="sm">
                Complete
              </Badge>
            )}
          </div>
          <div className="mt-5 space-y-5">{showStep()}</div>
        </div>
        <SheetFooter className="border-border flex-row items-center justify-between border-t px-6 py-4">
          <Button
            variant="tertiary"
            disabled={isMutating || isAcknowledgingCompletion}
            onClick={() => {
              if (currentStepIndex === 0) {
                onBackToInstallMethod();
              } else {
                setCurrentStepIndex((index) => index - 1);
              }
            }}
          >
            <Button.LeftIcon>
              <ArrowLeft className="h-3 w-3" />
            </Button.LeftIcon>
            <Button.Text>Back</Button.Text>
          </Button>
          <div className="flex items-center gap-2">
            <Button
              variant="tertiary"
              disabled={isMutating}
              onClick={onDismiss}
            >
              <Button.Text>Dismiss</Button.Text>
            </Button>
            {allEvidenceComplete && currentStepIndex === steps.length - 1 ? (
              <Button
                disabled={isMutating || isAcknowledgingCompletion}
                onClick={onDone}
              >
                <Button.Text>Done</Button.Text>
              </Button>
            ) : (
              <Button
                disabled={
                  isMutating ||
                  isAcknowledgingCompletion ||
                  !currentStep.complete ||
                  currentStepIndex === steps.length - 1
                }
                onClick={() => setCurrentStepIndex((index) => index + 1)}
              >
                <Button.Text>Next</Button.Text>
              </Button>
            )}
          </div>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

function PlatformMCPLoading(): JSX.Element {
  return (
    <div aria-label="Loading Platform MCP" className="mt-3 space-y-4">
      <Skeleton className="h-7 w-44" />
      <Skeleton className="h-4 w-full max-w-2xl" />
      <Skeleton className="h-40 w-full" />
    </div>
  );
}
