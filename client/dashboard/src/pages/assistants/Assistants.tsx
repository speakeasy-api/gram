import { Page } from "@/components/page-layout";
import { TabbedPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { CardContextMenu } from "@/components/card-context-menu";
import { Badge } from "@/components/ui/Badge";
import { McpEntityCard } from "@/components/mcp/McpEntityCard";
import type { Action } from "@/components/ui/MoreActions";
import { MoreActions } from "@/components/ui/MoreActions";
import { SearchBar } from "@/components/ui/SearchBar";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import {
  AssistantStatus,
  type Assistant,
} from "@gram/client/models/components/assistant.js";
import { AVAILABLE_MODELS } from "@/lib/models";
import { useAssistantsDeleteMutation } from "@gram/client/react-query/assistantsDelete.js";
import {
  invalidateAllAssistantsList,
  useAssistantsList,
} from "@gram/client/react-query/assistantsList.js";
import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowRight, Bot, Plus } from "lucide-react";
import { parseAsStringLiteral, useQueryState } from "nuqs";
import type { MouseEvent } from "react";
import { useMemo, useState } from "react";
import { Outlet } from "react-router";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";

import { AssistantsAuditLog } from "./AssistantAuditLog";
import { assistantAttachedServerSlugs } from "./assistantServers";
import { TriggersPanel } from "../triggers/Triggers";
import { Skeleton } from "@/components/ui/Skeleton";

const TOP_LEVEL_TABS = ["assistants", "triggers", "audit"] as const;

function stopLinkNavigation(e: MouseEvent<HTMLDivElement>) {
  e.preventDefault();
  e.stopPropagation();
}

export function AssistantsRoot(): JSX.Element {
  const project = useProject();
  return (
    <RequireScope scope="assistant:read" projectId={project.id} level="page">
      <Outlet />
    </RequireScope>
  );
}

function AssistantsEmptyState({
  onCreate,
  projectId,
}: {
  onCreate: () => void;
  projectId: string;
}) {
  return (
    <InlineEmptyState
      icon="bot"
      heading="No assistants yet"
      description="Give a model your MCP servers and skills, then put it to work in Slack or on a schedule."
      action={
        <RequireScope
          scope="assistant:write"
          resourceId={projectId}
          projectId={projectId}
          level="component"
          reason="You don't have permission to create assistants."
        >
          <Button onClick={onCreate}>
            <Button.LeftIcon>
              <Plus className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>New assistant</Button.Text>
          </Button>
        </RequireScope>
      }
    />
  );
}

export default function AssistantsIndex(): JSX.Element {
  const routes = useRoutes();
  const project = useProject();
  const { hasScope } = useRBAC();
  const canManageTriggers = hasScope("project:write", project.id);
  const [activeTab] = useQueryState(
    "tab",
    parseAsStringLiteral(TOP_LEVEL_TABS).withDefault("assistants"),
  );
  const visibleActiveTab =
    activeTab === "triggers" && !canManageTriggers ? "assistants" : activeTab;
  const { data, isLoading } = useAssistantsList(
    { gramProject: project.slug },
    undefined,
    {
      retry: false,
      throwOnError: false,
    },
  );

  const assistants = useMemo(() => data?.assistants ?? [], [data]);

  const [search, setSearch] = useState("");

  const filteredAssistants = useMemo(() => {
    const query = search.toLowerCase();
    return assistants.filter((assistant) => {
      if (!query) return true;
      return (
        assistant.name.toLowerCase().includes(query) ||
        assistant.model.toLowerCase().includes(query) ||
        modelLabel(assistant.model).toLowerCase().includes(query)
      );
    });
  }, [assistants, search]);

  const showSearch = !isLoading;
  const showNoMatches =
    !isLoading && search !== "" && filteredAssistants.length === 0;

  const content =
    !isLoading && assistants.length === 0 ? (
      <AssistantsEmptyState
        onCreate={() => routes.assistants.newAssistant.goTo()}
        projectId={project.id}
      />
    ) : (
      <Page.Section>
        <Page.Section.Title stage="beta">Assistants</Page.Section.Title>
        <Page.Section.Description className="max-w-xl">
          Secure assistants that connect through the MCPs and Skills your org
          already uses, with identity, guardrails, and audit built in. Deployed
          to Slack.
        </Page.Section.Description>
        <Page.Section.CTA>
          <RequireScope
            scope="assistant:write"
            resourceId={project.id}
            projectId={project.id}
            level="component"
            reason="You don't have permission to create assistants."
          >
            <Button onClick={() => routes.assistants.newAssistant.goTo()}>
              <Button.LeftIcon>
                <Plus className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>New assistant</Button.Text>
            </Button>
          </RequireScope>
        </Page.Section.CTA>
        <Page.Section.Body>
          {showSearch && (
            <SearchBar
              value={search}
              onChange={setSearch}
              placeholder="Search assistants..."
              className="mb-4"
            />
          )}
          <AssistantsBody
            isLoading={isLoading}
            showNoMatches={showNoMatches}
            search={search}
            assistants={filteredAssistants}
          />
        </Page.Section.Body>
      </Page.Section>
    );

  return (
    <TabbedPage
      activeTab={visibleActiveTab}
      tabs={[
        { value: "assistants", label: "Assistants", href: "?tab=assistants" },
        ...(canManageTriggers
          ? [{ value: "triggers", label: "Triggers", href: "?tab=triggers" }]
          : []),
        { value: "audit", label: "Activity", href: "?tab=audit" },
      ]}
    >
      {visibleActiveTab === "assistants" && content}
      {visibleActiveTab === "triggers" && <TriggersPanel />}
      {visibleActiveTab === "audit" && (
        <RequireScope scope="org:read" level="section">
          <AssistantsAuditLog />
        </RequireScope>
      )}
    </TabbedPage>
  );
}

function AssistantsBody({
  isLoading,
  showNoMatches,
  search,
  assistants,
}: {
  isLoading: boolean;
  showNoMatches: boolean;
  search: string;
  assistants: Assistant[];
}): JSX.Element {
  if (isLoading) {
    return (
      <div
        role="status"
        aria-label="Loading assistants"
        className="grid grid-cols-1 gap-6 xl:grid-cols-2"
      >
        <Skeleton className="h-[156px] w-full" />
        <Skeleton className="h-[156px] w-full" />
      </div>
    );
  }

  if (showNoMatches) {
    return (
      <Text muted className="py-8 text-center">
        No assistants matching &ldquo;{search}&rdquo;
      </Text>
    );
  }

  return (
    <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
      {assistants.map((assistant) => (
        <AssistantCard key={assistant.id} assistant={assistant} />
      ))}
    </div>
  );
}

function modelLabel(model: string): string {
  return AVAILABLE_MODELS.find((m) => m.value === model)?.label ?? model;
}

/** "Claude Sonnet 4.6 · 2 MCP servers": what it runs on and with. */
function assistantSummary(assistant: Assistant): string {
  const model = modelLabel(assistant.model);
  const servers = assistantAttachedServerSlugs(assistant).length;
  if (servers === 0) return `${model} · No MCP servers`;
  return `${model} · ${servers} MCP server${servers === 1 ? "" : "s"}`;
}

function AssistantCard({ assistant }: { assistant: Assistant }) {
  const routes = useRoutes();
  const project = useProject();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("assistant:write", assistant.id, project.id);
  const queryClient = useQueryClient();

  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const deleteAssistant = useAssistantsDeleteMutation({
    onSuccess: () => {
      setConfirmingDelete(false);
      void invalidateAllAssistantsList(queryClient);
    },
  });

  const paused = assistant.status === AssistantStatus.Paused;
  const actions: Action[] = canWrite
    ? [
        {
          label: "Delete",
          destructive: true,
          icon: "trash",
          onClick: () => setConfirmingDelete(true),
        },
      ]
    : [];

  return (
    <>
      <CardContextMenu actions={actions}>
        <routes.assistants.detail.Link
          params={[assistant.id]}
          className="focus-visible:ring-ring block h-full no-underline hover:no-underline focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
        >
          <McpEntityCard
            icon={<Bot className="text-muted-foreground size-8" />}
          >
            <div className="mb-2 flex items-start justify-between gap-2">
              <Text
                variant="subheading"
                as="div"
                className="text-md group-hover:text-primary flex-1 truncate normal-case transition-colors"
                title={assistant.name}
              >
                {assistant.name}
              </Text>
              {canWrite && (
                <div onClick={stopLinkNavigation}>
                  <MoreActions actions={actions} />
                </div>
              )}
            </div>
            <Text small muted className="mt-1 truncate">
              {assistantSummary(assistant)}
            </Text>
            <div className="mt-auto flex flex-wrap items-center justify-between gap-2 pt-2">
              <Badge variant={paused ? "warning" : "neutral"}>
                <Badge.Text>{paused ? "Paused" : "On"}</Badge.Text>
              </Badge>
              <div className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
                <span>Open</span>
                <ArrowRight className="h-3.5 w-3.5" />
              </div>
            </div>
          </McpEntityCard>
        </routes.assistants.detail.Link>
      </CardContextMenu>
      <ConfirmDialog
        open={confirmingDelete}
        onOpenChange={setConfirmingDelete}
        title={`Delete ${assistant.name}?`}
        description="It stops replying everywhere it runs. This can't be undone."
        confirmLabel="Delete assistant"
        pendingLabel="Deleting…"
        confirmVariant="destructive-primary"
        isPending={deleteAssistant.isPending}
        error={
          deleteAssistant.error
            ? "Couldn't delete the assistant. Try again."
            : undefined
        }
        onConfirm={() =>
          deleteAssistant.mutate({ request: { id: assistant.id } })
        }
      />
    </>
  );
}
