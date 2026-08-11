import { Page } from "@/components/page-layout";
import { TabbedPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { AssistantActivitySparkline } from "@/components/assistants/activity-sparkline";
import { AssistantOwner } from "@/components/assistants/assistant-owner";
import { AssistantStatusToggle } from "@/components/assistants/status-toggle";
import {
  BRAND_MESH_SURFACE_CLASS,
  BrandMeshLayers,
} from "@/components/brand-mesh";
import { CardContextMenu } from "@/components/card-context-menu";
import { Badge } from "@/components/ui/Badge";
import { Card } from "@/components/ui/Card";
import { Action, MoreActions } from "@/components/ui/MoreActions";
import { SearchBar } from "@/components/ui/SearchBar";
import { Text } from "@/components/ui/Text";
import { UpdatedAt } from "@/components/updated-at";
import { useRoutes } from "@/routes";
import { Assistant } from "@gram/client/models/components/assistant.js";
import { useAssistantsDeleteMutation } from "@gram/client/react-query/assistantsDelete.js";
import {
  invalidateAllAssistantsList,
  useAssistantsList,
} from "@gram/client/react-query/assistantsList.js";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { useQueryClient } from "@tanstack/react-query";
import { Bot, Boxes, Cpu, Plus } from "lucide-react";
import { parseAsStringLiteral, useQueryState } from "nuqs";
import { MouseEvent, useMemo, useState } from "react";
import { Outlet } from "react-router";

import { AssistantsAuditLog } from "./AssistantAuditLog";
import { TriggersPanel } from "../triggers/Triggers";

const TOP_LEVEL_TABS = ["assistants", "triggers", "audit"] as const;

function stopLinkNavigation(e: MouseEvent<HTMLDivElement>) {
  e.preventDefault();
  e.stopPropagation();
}

export function AssistantsRoot(): JSX.Element {
  return <Outlet />;
}

function AssistantsEmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center border border-dashed px-8 py-16">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center">
        <Icon name="bot" className="text-muted-foreground h-6 w-6" />
      </div>
      <Text variant="subheading" className="mb-1">
        No assistants yet
      </Text>
      <Text small muted className="mb-4 max-w-md text-center">
        Create an assistant to wire a model up to your MCP servers.
      </Text>
      <RequireScope
        scope={["project:write", "mcp:write"]}
        all
        level="component"
        reason="You don't have permission to create assistants."
      >
        <Button onClick={onCreate}>
          <Button.LeftIcon>
            <Plus className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Create Assistant</Button.Text>
        </Button>
      </RequireScope>
    </div>
  );
}

export default function AssistantsIndex(): JSX.Element {
  const routes = useRoutes();
  const [activeTab] = useQueryState(
    "tab",
    parseAsStringLiteral(TOP_LEVEL_TABS).withDefault("assistants"),
  );
  const { data, isLoading } = useAssistantsList(undefined, undefined, {
    retry: false,
    throwOnError: false,
  });

  const assistants = useMemo(() => data?.assistants ?? [], [data]);

  const [search, setSearch] = useState("");

  const filteredAssistants = useMemo(() => {
    const query = search.toLowerCase();
    return assistants.filter((assistant) => {
      if (!query) return true;
      return (
        assistant.name.toLowerCase().includes(query) ||
        assistant.model.toLowerCase().includes(query)
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
            scope={["project:write", "mcp:write"]}
            all
            level="component"
            reason="You don't have permission to create assistants."
          >
            <Button onClick={() => routes.assistants.newAssistant.goTo()}>
              <Button.LeftIcon>
                <Plus className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>New Assistant</Button.Text>
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
      activeTab={activeTab}
      tabs={[
        { value: "assistants", label: "Assistants", href: "?tab=assistants" },
        { value: "triggers", label: "Triggers", href: "?tab=triggers" },
        { value: "audit", label: "Activity", href: "?tab=audit" },
      ]}
    >
      {activeTab === "assistants" && content}
      {activeTab === "triggers" && (
        <RequireScope scope="project:write" level="section">
          <TriggersPanel />
        </RequireScope>
      )}
      {activeTab === "audit" && (
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
      <Stack align="center" justify="center" className="py-16">
        <Icon
          name="loader-circle"
          className="text-muted-foreground h-6 w-6 animate-spin"
        />
      </Stack>
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

function AssistantIcon() {
  return (
    <div className="border-border bg-surface-secondary-default flex h-12 w-12 items-center justify-center border">
      <Bot className="text-muted-foreground h-7 w-7" />
    </div>
  );
}

const MAX_VISIBLE_TOOLSETS = 3;

function AssistantToolsets({ assistant }: { assistant: Assistant }) {
  if (assistant.toolsets.length === 0) {
    return (
      <div className="flex items-center gap-1.5">
        <Boxes className="text-muted-foreground/70 size-3.5 shrink-0" />
        <Text muted small>
          No MCP servers
        </Text>
      </div>
    );
  }

  const visible = assistant.toolsets.slice(0, MAX_VISIBLE_TOOLSETS);
  const overflow = assistant.toolsets.length - visible.length;

  return (
    <div className="flex min-w-0 items-center gap-1.5">
      <Boxes className="text-muted-foreground/70 size-3.5 shrink-0" />
      <div className="flex min-w-0 flex-wrap items-center gap-1">
        {visible.map((toolset) => (
          <Badge
            key={toolset.toolsetSlug}
            variant="neutral"
            className="max-w-[10rem]"
            title={toolset.toolsetSlug}
          >
            <span className="min-w-0 truncate">{toolset.toolsetSlug}</span>
          </Badge>
        ))}
        {overflow > 0 && <Badge variant="neutral">+{overflow}</Badge>}
      </div>
    </div>
  );
}

function AssistantCard({ assistant }: { assistant: Assistant }) {
  const routes = useRoutes();
  const queryClient = useQueryClient();

  const deleteAssistant = useAssistantsDeleteMutation({
    onSuccess: () => {
      void invalidateAllAssistantsList(queryClient);
    },
  });

  const actions: Action[] = [
    {
      label: "Delete",
      destructive: true,
      icon: "trash",
      onClick: () => {
        if (confirm(`Delete assistant "${assistant.name}"?`)) {
          deleteAssistant.mutate({ request: { id: assistant.id } });
        }
      },
    },
  ];

  return (
    <CardContextMenu actions={actions}>
      <routes.assistants.detail.Link
        params={[assistant.id]}
        className="focus-visible:ring-ring block h-full no-underline hover:no-underline focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
      >
        <Card.Entity
          icon={<AssistantIcon />}
          iconRailClassName={BRAND_MESH_SURFACE_CLASS}
          overlay={<BrandMeshLayers seed={assistant.id} />}
        >
          {/* Header row: name + actions */}
          <div className="mb-3 flex items-start justify-between gap-2">
            <Text
              variant="subheading"
              as="div"
              className="text-md group-hover:text-primary flex-1 truncate normal-case transition-colors"
              title={assistant.name}
            >
              {assistant.name}
            </Text>
            <div onClick={stopLinkNavigation}>
              <MoreActions actions={actions} />
            </div>
          </div>

          {/* Metadata: model + MCP servers */}
          <div className="mb-3 flex flex-col gap-2">
            <div className="flex items-center gap-1.5">
              <Cpu className="text-muted-foreground/70 size-3.5 shrink-0" />
              <Text muted small className="truncate" title={assistant.model}>
                {assistant.model}
              </Text>
            </div>
            <AssistantToolsets assistant={assistant} />
            <AssistantOwner
              createdByUserId={assistant.createdByUserId}
              variant="card"
            />
          </div>

          {/* Footer row: status toggle + activity sparkline + last updated */}
          <div className="border-border/60 mt-auto flex items-center justify-between gap-2 border-t pt-3">
            <AssistantStatusToggle assistant={assistant} />
            <div className="flex items-center gap-2">
              <AssistantActivitySparkline assistantId={assistant.id} />
              <UpdatedAt date={new Date(assistant.updatedAt)} />
            </div>
          </div>
        </Card.Entity>
      </routes.assistants.detail.Link>
    </CardContextMenu>
  );
}
