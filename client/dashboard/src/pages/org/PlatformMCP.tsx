import {
  Alert,
  AlertDescription,
  AlertTitle,
  ErrorAlert,
} from "@/components/ui/Alert";
import { ArrowLeft, CircleCheck } from "lucide-react";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import {
  invalidatePlatformMCPOnboarding,
  usePlatformMCPOnboarding,
} from "@gram/client/react-query/platformMCPOnboarding.js";
import { useEffect, useRef, useState } from "react";

import { AgentPlatformPickerItem } from "@/pages/setup/components/agent-platform-picker-item";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import type { ClientFamily } from "@gram/client/models/components/recordinstallintentrequestbody.js";
import { CopyButton } from "@/components/ui/CopyButton";
import { Dialog } from "@/components/ui/Dialog";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { Navigate } from "react-router";
import { Page } from "@/components/page-layout";
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
import { useQueryClient } from "@tanstack/react-query";
import { useRecordPlatformMCPAgentConfigurationCopiedMutation } from "@gram/client/react-query/recordPlatformMCPAgentConfigurationCopied.js";
import { useRecordPlatformMCPInstallIntentMutation } from "@gram/client/react-query/recordPlatformMCPInstallIntent.js";
import { useStartPlatformMCPOnboardingMutation } from "@gram/client/react-query/startPlatformMCPOnboarding.js";

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
];

function starterPrompt(currentProjectSlug?: string): string {
  if (currentProjectSlug) {
    return `Help me add a reviewed MCP server from the catalogue to the project currently being set up: ${currentProjectSlug}. Show the available catalogue options, then ask me to choose one. Inspect the chosen server and collect only its declared non-secret configuration, including declared URL values where applicable. Register it privately, send me to the secure dashboard setup when needed, verify it is ready, and add it to this project's existing Default plugin. Do not ask me to paste API keys, tokens, passwords, OAuth codes, client secrets, or secret headers into chat. Do not ask me for the MCP server endpoint itself; use the reviewed catalogue entry selected for this project.`;
  }

  return "Help me add a reviewed MCP server to a project. Show the available catalogue options and eligible projects, then ask me to choose one of each. Inspect the chosen server and collect only its declared non-secret configuration, including declared URL values where applicable. Register it privately, send me to the secure dashboard setup when needed, verify it is ready, and add it to that project's existing Default plugin. Do not ask me to paste API keys, tokens, passwords, OAuth codes, client secrets, or secret headers into chat. Do not ask me for the MCP server endpoint itself; use the reviewed catalogue entry selected for this project.";
}

function manualConfiguration(mcpUrl: string): string {
  return JSON.stringify(
    {
      mcpServers: {
        "speakeasy-aicp-platform-mcp": {
          type: "http",
          url: mcpUrl,
        },
      },
    },
    null,
    2,
  );
}

export default function PlatformMCP(): JSX.Element | null {
  const { enabled: platformMcpDashboardEnabled, isLoading } =
    usePlatformMcpDashboardVisibility();

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
          <PlatformMCPOnboardingContent />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

export function PlatformMCPOnboardingContent({
  currentProjectSlug,
  embeddedInProjectSetup = false,
  onSetupComplete,
}: {
  currentProjectSlug?: string;
  embeddedInProjectSetup?: boolean;
  onSetupComplete?: () => void;
} = {}): JSX.Element {
  const queryClient = useQueryClient();
  const { fetch: authedFetch } = useFetcher();
  const [setupError, setSetupError] = useState<string | null>(null);
  const [accessError, setAccessError] = useState<string | null>(null);
  const [disableConfirmationOpen, setDisableConfirmationOpen] = useState(false);
  const [agentPickerOpen, setAgentPickerOpen] = useState(false);
  const [selectedClientID, setSelectedClientID] = useState<ClientFamily | null>(
    null,
  );
  const [setupSheetOpen, setSetupSheetOpen] = useState(false);
  const onboarding = usePlatformMCPOnboarding(
    { gramSession: "" },
    { sessionHeaderGramSession: "" },
    {
      throwOnError: false,
      staleTime: 10_000,
      refetchInterval: 5_000,
      refetchIntervalInBackground: false,
    },
  );

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
      setAccessError(null);
      setDisableConfirmationOpen(false);
      await Promise.all([
        invalidateAllProductFeatures(queryClient),
        invalidate(),
      ]);
    },
    onError: () => {
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
          featureName: FeatureName.PlatformMcp,
          enabled,
        },
      },
    });
  };

  if (onboarding.isLoading) {
    return <PlatformMCPLoading />;
  }

  if (onboarding.error || !onboarding.data) {
    return (
      <div className="mx-auto mt-8 flex max-w-xl flex-col gap-3">
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
  }

  const state = onboarding.data;
  if (!state.enabled) {
    return (
      <PlatformMCPUnavailable
        state={state}
        isMutating={setOrganizationAccess.isPending}
        accessError={accessError}
        onEnable={() => setPlatformMCPAccess(true)}
      />
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
        { onSuccess: () => setSetupSheetOpen(true) },
      );

    if (state.workflowActive) {
      recordIntent();
      return;
    }

    start.mutate(
      { security: { sessionHeaderGramSession: "" } },
      { onSuccess: recordIntent },
    );
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

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-6">
      <Page.Section>
        <Page.Section.Title stage="preview">
          Speakeasy AICP Platform MCP
        </Page.Section.Title>
        <Page.Section.Description className="max-w-3xl">
          Use an AI agent to add a reviewed MCP server to a project: connect the
          agent, choose a reviewed MCP server from the MCP Catalogue, complete
          any required setup, then make the server available through that
          project&apos;s existing Default plugin.
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

      <section
        className="border bg-card p-6"
        aria-labelledby="platform-mcp-setup"
      >
        <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <Text variant="subheading" id="platform-mcp-setup">
              {showManagement
                ? "Set up another agent"
                : "Set up Speakeasy AICP Platform MCP"}
            </Text>
            <Text muted small className="mt-2 max-w-2xl">
              {showManagement
                ? "Start a separate resumable checklist for another agent in this organization."
                : "Connect an agent, choose a reviewed MCP server, complete any required setup, and add it to the selected project's existing Default plugin."}
            </Text>
          </div>
          {!agentPickerOpen ? (
            <Button
              disabled={isMutating}
              onClick={() => setAgentPickerOpen(true)}
            >
              <Button.Text>Start setup</Button.Text>
            </Button>
          ) : null}
        </div>

        {agentPickerOpen ? (
          <div className="mt-6 border-t pt-6">
            <Text variant="subheading">Choose an agent</Text>
            <Text muted small className="mt-2 max-w-2xl">
              Select the agent you&apos;ll use to set up Speakeasy AICP Platform
              MCP. We&apos;ll show that agent&apos;s installation instructions
              first.
            </Text>
            <div className="mt-4 space-y-2">
              {clients.map((client) => (
                <AgentPlatformPickerItem
                  key={client.id}
                  platformId={client.id.replaceAll("_", "-")}
                  name={client.label}
                  description={client.description}
                  disabled={isMutating}
                  onClick={() => selectAgentForSetup(client)}
                />
              ))}
            </div>
            <Button
              variant="secondary"
              className="mt-4"
              disabled={isMutating}
              onClick={() => setAgentPickerOpen(false)}
            >
              Cancel
            </Button>
          </div>
        ) : null}
      </section>

      {state.workflowActive && (
        <PlatformMCPSetupSheet
          open={setupSheetOpen}
          onOpenChange={setSetupSheetOpen}
          state={state}
          currentProjectSlug={currentProjectSlug}
          activeClient={activeClient}
          isMutating={isMutating}
          setupError={setupError}
          onConfigurationCopied={() => {
            setSetupSheetOpen(true);
            recordConfigurationCopied.mutate({
              security: { sessionHeaderGramSession: "" },
            });
          }}
          onContinueSecureSetup={() => void continueSecureSetup()}
          onDismiss={() =>
            dismiss.mutate({ security: { sessionHeaderGramSession: "" } })
          }
          onDone={() => {
            setSetupSheetOpen(false);
            onSetupComplete?.();
          }}
        />
      )}

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
    <div className="mx-auto mt-8 max-w-2xl">
      <Alert variant="warning">
        <div>
          <AlertTitle>
            Platform MCP is not enabled for this organization
          </AlertTitle>
          <AlertDescription>
            No one in this organization can currently connect to or use Platform
            MCP. Existing connections and project distributions are retained but
            remain unavailable until an organization administrator enables
            access.
          </AlertDescription>
        </div>
      </Alert>
      {state.repairAction === "enable_platform_mcp" && (
        <div className="mt-4 flex flex-col gap-3 rounded-xl border bg-card p-5">
          <div>
            <Text variant="subheading">Organization-wide access is off</Text>
            <Text muted small className="mt-1 max-w-2xl">
              Enable access to allow organization administrators to connect
              agents and use the Platform MCP workflow again. This does not
              create or restore any connection automatically.
            </Text>
          </div>
          <Button
            className="self-start"
            disabled={isMutating}
            onClick={onEnable}
          >
            <Button.Text>
              {isMutating ? "Enabling…" : "Enable Platform MCP"}
            </Button.Text>
          </Button>
          {accessError && (
            <ErrorAlert
              title="Could not enable Platform MCP"
              error={accessError}
            />
          )}
        </div>
      )}
    </div>
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
  const connectionStatus = state.connectionReady
    ? "Ready"
    : state.connectionAuthorized
      ? "Authorized"
      : "Not connected";
  const connectionVariant = state.connectionReady
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
              "add_platform_mcp_to_default_plugin",
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
            readiness, and add a ready MCP only to the chosen project&apos;s
            existing Default plugin.
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

function ManualInstallCard({
  client,
  state,
  onConfigurationCopied,
}: {
  client: (typeof clients)[number];
  state: PlatformMCPOnboardingState;
  onConfigurationCopied: () => void;
}): JSX.Element {
  return (
    <div className="rounded-xl border bg-card p-6">
      <div className="space-y-5">
        <div>
          <Text variant="subheading">Set up {client.label}</Text>
          <Text muted small className="mt-1 max-w-2xl">
            Copy this complete JSON object into your client&apos;s MCP
            configuration, restart the client, and complete the browser sign-in
            it opens. We&apos;ll continue automatically when authentication is
            detected.
          </Text>
        </div>

        <CopyValue
          label={`${client.label} configuration`}
          value={manualConfiguration(state.mcpUrl)}
          codeBlock
          onCopy={onConfigurationCopied}
        />
      </div>
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
  isMutating,
  setupError,
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
  isMutating: boolean;
  setupError: string | null;
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
  const wasOpenRef = useRef(open);
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
      setCurrentStepIndex(evidenceStepIndex);
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
              Install Platform MCP in {activeClient.label}, then restart the
              agent and complete the browser sign-in it opens. Once it is
              authenticated, the next step provides a guided prompt to configure
              an MCP for distribution.
            </Text>
            <ManualInstallCard
              client={activeClient}
              state={state}
              onConfigurationCopied={onConfigurationCopied}
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
          <SheetTitle>Set up Speakeasy AICP Platform MCP</SheetTitle>
          <SheetDescription>
            Complete Platform MCP setup one lifecycle step at a time.
          </SheetDescription>
        </SheetHeader>
        <div className="flex items-center gap-1.5 px-6 pt-6 pr-14">
          {steps.map((step, index) => (
            <button
              key={step.title}
              type="button"
              disabled={isAcknowledgingCompletion || index > currentStepIndex}
              onClick={() => setCurrentStepIndex(index)}
              className={cn(
                "h-1 rounded-full transition-all",
                index === currentStepIndex
                  ? "bg-foreground w-6"
                  : index < currentStepIndex || step.complete
                    ? "bg-foreground/40 hover:bg-foreground/60 w-4 cursor-pointer"
                    : "bg-border w-4 cursor-not-allowed",
              )}
              aria-label={`Step ${index + 1}: ${step.title}${step.complete ? ", complete" : ""}`}
            />
          ))}
          <span className="text-muted-foreground ml-auto text-[11px] tabular-nums">
            {currentStepIndex + 1}/{steps.length}
          </span>
        </div>
        <div className="flex-1 overflow-y-auto px-6 py-6">
          <p className="text-eyebrow">Step {currentStepIndex + 1}</p>
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
            disabled={
              isMutating || isAcknowledgingCompletion || currentStepIndex === 0
            }
            onClick={() => setCurrentStepIndex((index) => index - 1)}
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
            {allEvidenceComplete ? (
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
