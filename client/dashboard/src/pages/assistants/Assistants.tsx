import { TopUpCTA, UsageProgress } from "@/components/billing/usage-controls";
import { Page } from "@/components/page-layout";
import { getGradientColors } from "@/components/gradient-colors";
import { RequireScope } from "@/components/require-scope";
import { AssistantActivitySparkline } from "@/components/assistants/activity-sparkline";
import { AssistantStatusToggle } from "@/components/assistants/status-toggle";
import { CardContextMenu } from "@/components/card-context-menu";
import { Badge } from "@/components/ui/badge";
import { DotCard } from "@/components/ui/dot-card";
import { Action, MoreActions } from "@/components/ui/more-actions";
import { SearchBar } from "@/components/ui/search-bar";
import { Skeleton } from "@/components/ui/skeleton";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { UpdatedAt } from "@/components/updated-at";
import { useProductTier } from "@/hooks/useProductTier";
import { useRoutes } from "@/routes";
import { Assistant } from "@gram/client/models/components/assistant.js";
import {
  invalidateAllAssistantsList,
  useAssistantsDeleteMutation,
  useAssistantsList,
  useGetPeriodUsage,
} from "@gram/client/react-query/index.js";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Bot, Boxes, Cpu, Info, Plus } from "lucide-react";
import { parseAsStringLiteral, useQueryState } from "nuqs";
import { MouseEvent, useMemo, useState } from "react";
import { Outlet } from "react-router";

import { AssistantsAuditLog } from "./AssistantAuditLog";
import { TriggersPanel } from "../triggers/Triggers";

const TOP_LEVEL_TABS = ["assistants", "triggers", "audit"] as const;
type TopLevelTab = (typeof TOP_LEVEL_TABS)[number];

function toTopLevelTab(value: string): TopLevelTab {
  return (TOP_LEVEL_TABS as readonly string[]).includes(value)
    ? (value as TopLevelTab)
    : "assistants";
}

function stopLinkNavigation(e: MouseEvent<HTMLDivElement>) {
  e.preventDefault();
  e.stopPropagation();
}

export function AssistantsRoot(): JSX.Element {
  return <Outlet />;
}

function AssistantsEmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <Icon name="bot" className="text-muted-foreground h-6 w-6" />
      </div>
      <Type variant="subheading" className="mb-1">
        No assistants yet
      </Type>
      <Type small muted className="mb-4 max-w-md text-center">
        Create an assistant to wire a model up to your MCP servers.
      </Type>
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
  const [activeTab, setActiveTab] = useQueryState(
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

  const showSearch = !isLoading && (assistants.length > 6 || search !== "");
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
          Openclaw-inspired secure Assistants. Every assistant connects through
          the MCPs and Skills your org already uses, with identity, guardrails,
          and audit built in. Deployed to Slack.
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
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Tabs
          value={activeTab}
          onValueChange={(value) => void setActiveTab(toTopLevelTab(value))}
          className="flex w-full flex-col"
        >
          <div className="border-b">
            <TabsList className="h-auto gap-6 rounded-none bg-transparent p-0">
              <PageTabsTrigger value="assistants">Assistants</PageTabsTrigger>
              <PageTabsTrigger value="triggers">Triggers</PageTabsTrigger>
              <PageTabsTrigger value="audit">Activity</PageTabsTrigger>
            </TabsList>
          </div>
          <TabsContent
            value="assistants"
            className="mt-6 flex w-full flex-col gap-4"
          >
            {content}
            <UsageSection />
          </TabsContent>
          <TabsContent value="triggers" className="mt-6 w-full">
            <TriggersPanel />
          </TabsContent>
          <TabsContent value="audit" className="mt-6 w-full">
            <RequireScope scope="org:read" level="section">
              <AssistantsAuditLog />
            </RequireScope>
          </TabsContent>
        </Tabs>
      </Page.Body>
    </Page>
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
      <Type muted className="py-8 text-center">
        No assistants matching &ldquo;{search}&rdquo;
      </Type>
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

function UsageSection() {
  const productTier = useProductTier();
  const { data: periodUsage, isError } = useGetPeriodUsage(
    undefined,
    undefined,
    { throwOnError: false },
  );

  if (isError) return null;

  return (
    <Page.Section>
      <Page.Section.Title>Assistant Credits</Page.Section.Title>
      <Page.Section.Description>
        Credits consumed by assistant runs this billing period. Each turn debits
        credits based on the underlying model's cost.
      </Page.Section.Description>
      <RequireScope scope="org:admin" level="section">
        <TopUpCTA />
      </RequireScope>
      <Page.Section.Body>
        <Stack gap={3} className="mb-6">
          <Stack direction="horizontal" align="center" gap={1}>
            <Type variant="body" className="font-medium">
              Credits
            </Type>
            <SimpleTooltip tooltip="Credits track model usage across assistants and chat. 1 credit ≈ $1 of model cost.">
              <Info className="text-muted-foreground h-4 w-4" />
            </SimpleTooltip>
          </Stack>
          {periodUsage ? (
            <UsageProgress
              value={periodUsage.credits}
              included={periodUsage.includedCredits || 1}
              overageIncrement={periodUsage.includedCredits || 1}
              noMax={productTier === "enterprise"}
            />
          ) : (
            <Skeleton className="h-4 w-full" />
          )}
        </Stack>
      </Page.Section.Body>
    </Page.Section>
  );
}

// Each assistant gets a deterministic gradient tile behind its Bot icon,
// derived from its id via the same hash that powers project avatar colors.
function AssistantIcon({ assistant }: { assistant: Pick<Assistant, "id"> }) {
  const colors = getGradientColors(assistant.id);
  return (
    <div
      className="flex h-12 w-12 items-center justify-center rounded-lg bg-gradient-to-br"
      style={{
        backgroundImage: `linear-gradient(${colors.angle}deg, ${colors.from}, ${colors.to})`,
      }}
    >
      <Bot className="h-7 w-7 text-white" />
    </div>
  );
}

const MAX_VISIBLE_TOOLSETS = 3;

function AssistantToolsets({ assistant }: { assistant: Assistant }) {
  if (assistant.toolsets.length === 0) {
    return (
      <div className="flex items-center gap-1.5">
        <Boxes className="text-muted-foreground/70 size-3.5 shrink-0" />
        <Type muted small>
          No MCP servers
        </Type>
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
            variant="outline"
            className="max-w-[10rem]"
            title={toolset.toolsetSlug}
          >
            <span className="min-w-0 truncate">{toolset.toolsetSlug}</span>
          </Badge>
        ))}
        {overflow > 0 && <Badge variant="outline">+{overflow}</Badge>}
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
        className="focus-visible:ring-ring block h-full rounded-xl no-underline focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
      >
        <DotCard icon={<AssistantIcon assistant={assistant} />}>
          {/* Header row: name + actions */}
          <div className="mb-3 flex items-start justify-between gap-2">
            <Type
              variant="subheading"
              as="div"
              className="text-md group-hover:text-primary flex-1 truncate normal-case transition-colors"
              title={assistant.name}
            >
              {assistant.name}
            </Type>
            <div onClick={stopLinkNavigation}>
              <MoreActions actions={actions} />
            </div>
          </div>

          {/* Metadata: model + MCP servers */}
          <div className="mb-3 flex flex-col gap-2">
            <div className="flex items-center gap-1.5">
              <Cpu className="text-muted-foreground/70 size-3.5 shrink-0" />
              <Type muted small className="truncate" title={assistant.model}>
                {assistant.model}
              </Type>
            </div>
            <AssistantToolsets assistant={assistant} />
          </div>

          {/* Footer row: status toggle + activity sparkline + last updated */}
          <div className="border-border/60 mt-auto flex items-center justify-between gap-2 border-t pt-3">
            <AssistantStatusToggle assistant={assistant} />
            <div className="flex items-center gap-2">
              <AssistantActivitySparkline assistantId={assistant.id} />
              <UpdatedAt date={new Date(assistant.updatedAt)} />
            </div>
          </div>
        </DotCard>
      </routes.assistants.detail.Link>
    </CardContextMenu>
  );
}
