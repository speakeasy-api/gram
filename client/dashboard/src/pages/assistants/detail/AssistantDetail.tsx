import { AssistantOwner } from "@/components/assistants/assistant-owner";
import { AssistantSessionsList } from "@/components/assistants/sessions-list";
import {
  DangerSettingsSection,
  SettingsSection,
} from "@/components/detail/settings-section";
import { McpDetailFrame } from "@/components/mcp/McpDetailFrame";
import { HEADER_CONTROL_BUTTON } from "@/components/mcp/headerControl";
import type {
  McpDetailChrome,
  McpReadinessCheck,
} from "@/components/mcp/mcpDetailChrome";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Skeleton } from "@/components/ui/Skeleton";
import { StatusDot } from "@/components/ui/StatusDot";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { AVAILABLE_MODELS } from "@/lib/models";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { TriggerDialog } from "@/pages/triggers/Triggers";
import type { Assistant } from "@gram/client/models/components/assistant.js";
import { AssistantStatus } from "@gram/client/models/components/assistant.js";
import type { TriggerInstance } from "@gram/client/models/components/triggerinstance.js";
import { useAssistantsDeleteMutation } from "@gram/client/react-query/assistantsDelete.js";
import { invalidateAllAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { useAssistantsUpdateMutation } from "@gram/client/react-query/assistantsUpdate.js";
import { useListEnvironments } from "@gram/client/react-query/listEnvironments.js";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { useTriggers } from "@gram/client/react-query/triggers.js";
import { useQueryClient } from "@tanstack/react-query";
import {
  Bot,
  Boxes,
  Check,
  ChevronDown,
  Cpu,
  Fingerprint,
  Loader2,
  MessagesSquare,
  Radio,
  Sparkles,
  Settings,
} from "lucide-react";
import { parseAsStringLiteral, useQueryState } from "nuqs";
import { useMemo, useState } from "react";
import { useParams } from "react-router";
import { toast } from "sonner";
import { AssistantDraftProvider } from "./AssistantDraftContext";
import { AssistantIdentitySettings } from "./AssistantIdentitySettings";
import { AssistantMCPServersSection } from "./AssistantMCPServersSection";
import { AssistantOverviewSettings } from "./AssistantOverviewSettings";
import { AssistantSkillsSection } from "./AssistantSkillsSection";
import { AssistantTriggersList } from "./AssistantTriggersList";
import { AssistantUpstreamCredentials } from "./AssistantUpstreamCredentials";
import { InstructionsSection } from "./InstructionsSection";
import { Section } from "./PanelSection";
import {
  ASSISTANT_INSTRUCTIONS_SECTION_ID,
  ASSISTANT_SLACK_SECTION_ID,
  ASSISTANT_TRIGGERS_SECTION_ID,
} from "./sectionIds";
import { splitInstructions } from "./sections";
import { slackConnection, type SlackConnection } from "./slackConnection";
import { SlackSection } from "./SlackSection";
import { useAssistantDraft } from "./useAssistantDraft";

const TABS = [
  "sessions",
  "mcp-servers",
  "skills",
  "channels",
  "identity",
  "settings",
] as const;
type Tab = (typeof TABS)[number];

export function AssistantDetail(): JSX.Element {
  const { assistantId = "" } = useParams();
  return (
    <AssistantDraftProvider initialAssistantId={assistantId}>
      <AssistantDetailInner />
    </AssistantDraftProvider>
  );
}

function AssistantDetailInner(): JSX.Element {
  const draft = useAssistantDraft();
  const assistant = draft.assistant;
  const [tab] = useQueryState(
    "tab",
    parseAsStringLiteral(TABS).withDefault("sessions"),
  );

  const triggersQuery = useTriggers(undefined, undefined, {
    retry: false,
    throwOnError: false,
  });
  const toolsetsQuery = useListToolsets(undefined, undefined, {
    throwOnError: false,
  });
  const environmentsQuery = useListEnvironments(undefined, undefined, {
    throwOnError: false,
  });

  const triggers = useMemo(
    () =>
      (triggersQuery.data?.triggers ?? []).filter(
        (t) => t.targetKind === "assistant" && t.targetRef === assistant?.id,
      ),
    [triggersQuery.data, assistant?.id],
  );
  const slack = useMemo(
    () =>
      assistant
        ? slackConnection(
            assistant,
            triggers,
            toolsetsQuery.data?.toolsets ?? [],
            environmentsQuery.data?.environments ?? [],
          )
        : null,
    [assistant, triggers, toolsetsQuery.data, environmentsQuery.data],
  );
  // Empty fallbacks are only safe once every list has loaded; a failed list
  // would hide an existing Slack setup and invite a duplicate.
  const settled =
    triggersQuery.isSuccess &&
    toolsetsQuery.isSuccess &&
    environmentsQuery.isSuccess;
  const loadFailed =
    triggersQuery.isError || toolsetsQuery.isError || environmentsQuery.isError;

  const chrome = useMemo(
    () =>
      assistant && slack
        ? assistantChrome(assistant, tab, triggers, slack, settled)
        : null,
    [assistant, tab, triggers, slack, settled],
  );

  return (
    <McpDetailFrame
      chrome={chrome}
      breadcrumbs={{
        substitutions: assistant ? { [assistant.id]: assistant.name } : {},
        skipSegments: [],
      }}
    >
      {!assistant || !slack ? (
        <div
          role="status"
          aria-label="Loading assistant"
          className="flex flex-col gap-6"
        >
          <Skeleton className="h-40 w-full" />
          <Skeleton className="h-64 w-full" />
        </div>
      ) : (
        <div className="flex flex-col gap-10">
          {tab === "mcp-servers" && <McpServersTab assistant={assistant} />}
          {tab === "skills" && <SkillsTab />}
          {tab === "channels" && (
            <ChannelsTab
              assistant={assistant}
              slack={slack}
              triggers={triggers}
              settled={settled}
              loadFailed={loadFailed}
            />
          )}
          {tab === "identity" && <IdentityTab assistant={assistant} />}
          {tab === "sessions" && (
            <AssistantSessionsList assistantId={assistant.id} />
          )}
          {tab === "settings" && <SettingsTab assistant={assistant} />}
        </div>
      )}
    </McpDetailFrame>
  );
}

function assistantChrome(
  assistant: Assistant,
  tab: Tab,
  triggers: TriggerInstance[],
  slack: SlackConnection,
  settled: boolean,
): McpDetailChrome {
  const parts = splitInstructions(assistant.instructions);
  const tools =
    assistant.toolsets.length +
    (assistant.mcpServers?.length ?? 0) +
    assistant.skills.length;

  const readinessChecks: McpReadinessCheck[] = [
    {
      key: "instructions",
      label: "Instructions",
      description: "Describe what it does.",
      ready: parts.Tasks !== "" || parts.preamble !== "",
      href: `?tab=settings#${ASSISTANT_INSTRUCTIONS_SECTION_ID}`,
    },
    {
      key: "tools",
      label: "Tools",
      description: "Add an MCP server or skill.",
      ready: tools > 0,
      href: `?tab=mcp-servers`,
    },
    {
      key: "channel",
      label: "Trigger",
      description: "Connect Slack or add a trigger.",
      ready: settled ? triggers.length > 0 || slack.connected : null,
      pending: !settled,
      href: `?tab=channels`,
    },
  ];
  if (slack.connected) {
    readinessChecks.push({
      key: "slack-app",
      label: "Slack app",
      description: "Install the app and add credentials.",
      ready: settled ? slack.missing.length === 0 : null,
      pending: !settled,
      chipLabel: "Install Slack app",
      href: `?tab=channels#${ASSISTANT_SLACK_SECTION_ID}`,
    });
  }

  const tabHref = (key: Tab) => (key === "sessions" ? "?" : `?tab=${key}`);
  const items = (
    [
      { key: "sessions", title: "Sessions", Icon: MessagesSquare },
      { key: "mcp-servers", title: "MCP servers", Icon: Boxes },
      { key: "skills", title: "Skills", Icon: Sparkles },
      { key: "channels", title: "Channels", Icon: Radio },
      { key: "identity", title: "Identity", Icon: Fingerprint },
      { key: "settings", title: "Settings", Icon: Settings },
    ] as const
  ).map((item) => ({
    ...item,
    href: tabHref(item.key),
    active: item.key === tab,
  }));

  return {
    name: assistant.name,
    icon: (className) => (
      <Bot className={cn(className, "text-muted-foreground")} />
    ),
    kindLabel: undefined,
    eyebrow: "Assistant",
    navLabel: "Assistant sections",
    subtitle: (
      <>
        <span className="flex items-center gap-1.5">
          <Cpu className="size-3.5" />
          {AVAILABLE_MODELS.find((m) => m.value === assistant.model)?.label ??
            assistant.model}
        </span>
        <AssistantOwner
          createdByUserId={assistant.createdByUserId}
          variant="card"
        />
      </>
    ),
    statusControl: <AssistantHeaderStatus assistant={assistant} />,
    primaryUrl: undefined,
    readinessChecks,
    installLinks: [],
    items,
  };
}

const STATUS_OPTIONS = [
  {
    on: true,
    label: "On",
    description: "Responds to Slack and triggers.",
  },
  {
    on: false,
    label: "Paused",
    description: "Ignores everything.",
  },
] as const;

/** The header's on/paused control, confirming before it pauses. */
function AssistantHeaderStatus({
  assistant,
}: {
  assistant: Assistant;
}): JSX.Element {
  const project = useProject();
  const draft = useAssistantDraft();
  const queryClient = useQueryClient();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("assistant:write", assistant.id, project.id);
  const [confirmPause, setConfirmPause] = useState(false);
  const update = useAssistantsUpdateMutation({
    onSuccess: (updated) => {
      draft.setAssistant(updated);
      void invalidateAllAssistantsList(queryClient);
      setConfirmPause(false);
    },
    onError: () => toast.error("Couldn't change the assistant's status."),
  });
  // Shows the requested state while it saves, so the control answers at once.
  const requested = update.variables?.request.updateAssistantForm.status;
  const on =
    (update.isPending && requested ? requested : assistant.status) ===
    AssistantStatus.Active;
  const setOn = (next: boolean) =>
    update.mutate({
      request: {
        updateAssistantForm: {
          id: assistant.id,
          status: next ? AssistantStatus.Active : AssistantStatus.Paused,
        },
      },
    });

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild disabled={!canWrite}>
          <button
            type="button"
            className={HEADER_CONTROL_BUTTON}
            aria-busy={update.isPending}
          >
            {update.isPending ? (
              <Loader2
                aria-label="Saving"
                className="text-muted-foreground size-3 shrink-0 animate-spin"
              />
            ) : (
              <StatusDot tone={on ? "success" : "warning"} />
            )}
            <span className="min-w-0 flex-1 truncate text-left">
              {on ? "On" : "Paused"}
            </span>
            <ChevronDown className="text-muted-foreground size-3.5 shrink-0" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-[300px] p-1">
          {STATUS_OPTIONS.map((option) => (
            <DropdownMenuItem
              key={option.label}
              disabled={update.isPending}
              className="flex items-start gap-2 py-2"
              onSelect={() => {
                if (option.on === on) return;
                if (option.on) setOn(true);
                else setConfirmPause(true);
              }}
            >
              <StatusDot
                tone={option.on ? "success" : "warning"}
                className="mt-1.5"
              />
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="text-sm font-medium">{option.label}</span>
                <span className="text-muted-foreground text-xs">
                  {option.description}
                </span>
              </span>
              {option.on === on ? (
                <Check className="mt-0.5 size-4 shrink-0" />
              ) : null}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
      <ConfirmDialog
        open={confirmPause}
        onOpenChange={setConfirmPause}
        title={`Pause ${assistant.name}?`}
        description="It ignores Slack and triggers until turned back on."
        confirmLabel="Pause"
        pendingLabel="Pausing…"
        isPending={update.isPending}
        onConfirm={() => setOn(false)}
      />
    </>
  );
}

function McpServersTab({ assistant }: { assistant: Assistant }): JSX.Element {
  const draft = useAssistantDraft();
  return (
    <AssistantMCPServersSection
      assistant={assistant}
      onUpdated={() => void draft.refetchAssistant()}
    />
  );
}

function SkillsTab(): JSX.Element {
  const project = useProject();
  return (
    <RequireScope scope="skill:read" resourceId={project.id} level="section">
      <AssistantSkillsSection />
    </RequireScope>
  );
}

function SettingsTab({ assistant }: { assistant: Assistant }): JSX.Element {
  const project = useProject();
  const draft = useAssistantDraft();
  return (
    <>
      <InstructionsSection
        assistant={assistant}
        onUpdated={() => void draft.refetchAssistant()}
      />
      <AssistantOverviewSettings
        assistant={assistant}
        onUpdated={() => void draft.refetchAssistant()}
      />
      <RequireScope
        scope="assistant:write"
        resourceId={assistant.id}
        projectId={project.id}
        level="component"
      >
        <DeleteAssistantSection assistant={assistant} />
      </RequireScope>
    </>
  );
}

function DeleteAssistantSection({
  assistant,
}: {
  assistant: Assistant;
}): JSX.Element {
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const del = useAssistantsDeleteMutation({
    onSuccess: () => {
      void invalidateAllAssistantsList(queryClient);
      routes.assistants.goTo();
    },
  });
  return (
    <DangerSettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Danger zone</SettingsSection.Title>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="min-w-0 space-y-1">
              <Text small className="font-medium">
                Delete assistant
              </Text>
              <Text small muted>
                This can't be undone.
              </Text>
            </div>
            <Button
              variant="destructive-secondary"
              onClick={() => setConfirming(true)}
            >
              Delete assistant
            </Button>
          </div>
        </SettingsSection.Body>
      </SettingsSection.Panel>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={`Delete ${assistant.name}?`}
        description="This can't be undone."
        confirmLabel="Delete assistant"
        pendingLabel="Deleting…"
        confirmVariant="destructive-primary"
        isPending={del.isPending}
        error={
          del.error ? "Couldn't delete the assistant. Try again." : undefined
        }
        onConfirm={() => del.mutate({ request: { id: assistant.id } })}
      />
    </DangerSettingsSection>
  );
}

function ChannelsTab({
  assistant,
  slack,
  triggers,
  settled,
  loadFailed,
}: {
  assistant: Assistant;
  slack: SlackConnection;
  triggers: TriggerInstance[];
  settled: boolean;
  loadFailed: boolean;
}): JSX.Element {
  const project = useProject();
  const { hasScope } = useRBAC();
  // Triggers are project resources: the same check the Triggers page uses.
  const canManageTriggers = hasScope("project:write", project.id);
  const [editing, setEditing] = useState<TriggerInstance | null>(null);
  const [creating, setCreating] = useState(false);

  if (!settled) {
    return loadFailed ? (
      <Text small muted>
        Couldn't load this assistant's channels. Reload the page to try again.
      </Text>
    ) : (
      <div role="status" aria-label="Loading channels">
        <Skeleton className="h-40 w-full" />
      </div>
    );
  }

  return (
    <>
      <SlackSection assistant={assistant} slack={slack} triggers={triggers} />
      <Section
        id={ASSISTANT_TRIGGERS_SECTION_ID}
        title="Triggers"
        action={
          canManageTriggers ? (
            <Button variant="secondary" onClick={() => setCreating(true)}>
              Add trigger
            </Button>
          ) : undefined
        }
      >
        <AssistantTriggersList
          triggers={triggers}
          onEdit={canManageTriggers ? setEditing : undefined}
          emptyText="No triggers."
        />
      </Section>
      <TriggerDialog
        key={assistant.id}
        open={creating || editing !== null}
        onOpenChange={(open) => {
          if (open) return;
          setCreating(false);
          setEditing(null);
        }}
        editingTrigger={editing}
        fixedTarget={{
          kind: "assistant",
          ref: assistant.id,
          display: assistant.name,
        }}
        excludeDefinitions={["slack"]}
      />
    </>
  );
}

function IdentityTab({ assistant }: { assistant: Assistant }): JSX.Element {
  const project = useProject();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("assistant:write", assistant.id, project.id);
  return (
    <>
      <Section title="Agent identity">
        <AssistantIdentitySettings key={assistant.id} assistant={assistant} />
      </Section>
      <AssistantUpstreamCredentials
        assistantId={assistant.id}
        canWrite={canWrite}
      />
    </>
  );
}
