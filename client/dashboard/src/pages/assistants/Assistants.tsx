import { TopUpCTA, UsageProgress } from "@/components/billing/usage-controls";
import { Page } from "@/components/page-layout";
import { getProjectColors } from "@/components/project-colors";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/badge";
import { DotCard } from "@/components/ui/dot-card";
import { MoreActions } from "@/components/ui/more-actions";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { UpdatedAt } from "@/components/updated-at";
import { useProductTier } from "@/hooks/useProductTier";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import {
  Assistant,
  AssistantStatus,
} from "@gram/client/models/components/assistant.js";
import {
  invalidateAllAssistantsList,
  useAssistantsDeleteMutation,
  useAssistantsList,
  useAssistantsUpdateMutation,
  useGetPeriodUsage,
} from "@gram/client/react-query/index.js";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Bot, Boxes, Cpu, Info, Plus } from "lucide-react";
import { MouseEvent } from "react";
import { Outlet } from "react-router";
import { toast } from "sonner";

function stopLinkNavigation(e: MouseEvent<HTMLDivElement>) {
  e.preventDefault();
  e.stopPropagation();
}

export function AssistantsRoot(): JSX.Element {
  return <Outlet />;
}

function StatusToggle({ assistant }: { assistant: Assistant }) {
  const queryClient = useQueryClient();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("project:write");
  const isActive = assistant.status === AssistantStatus.Active;

  const updateAssistant = useAssistantsUpdateMutation({
    onSuccess: () => {
      void invalidateAllAssistantsList(queryClient);
    },
    onError: () => {
      toast.error("Failed to update assistant status");
    },
  });

  const handleToggle = () => {
    updateAssistant.mutate({
      request: {
        updateAssistantForm: {
          id: assistant.id,
          status: isActive ? AssistantStatus.Paused : AssistantStatus.Active,
        },
      },
    });
  };

  return (
    <Stack direction="horizontal" gap={2} align="center">
      <div onClick={stopLinkNavigation}>
        <Switch
          checked={isActive}
          onCheckedChange={handleToggle}
          disabled={!canWrite || updateAssistant.isPending}
          aria-label={`${isActive ? "Pause" : "Activate"} assistant ${assistant.name}`}
        />
      </div>
      <Type small muted>
        {isActive ? "Active" : "Paused"}
      </Type>
    </Stack>
  );
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
  const { data, isLoading } = useAssistantsList(undefined, undefined, {
    retry: false,
    throwOnError: false,
  });

  const assistants = data?.assistants ?? [];

  const content =
    !isLoading && assistants.length === 0 ? (
      <AssistantsEmptyState
        onCreate={() => routes.assistants.newAssistant.goTo()}
      />
    ) : (
      <Page.Section>
        <Page.Section.Title stage="preview">Assistants</Page.Section.Title>
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
          {isLoading ? (
            <Stack align="center" justify="center" className="py-16">
              <Icon
                name="loader-circle"
                className="text-muted-foreground h-6 w-6 animate-spin"
              />
            </Stack>
          ) : (
            <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
              {assistants.map((assistant) => (
                <AssistantCard key={assistant.id} assistant={assistant} />
              ))}
            </div>
          )}
        </Page.Section.Body>
      </Page.Section>
    );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        {content}
        <UsageSection />
      </Page.Body>
    </Page>
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
  const colors = getProjectColors(assistant.id);
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
            className="max-w-[10rem] truncate"
          >
            {toolset.toolsetSlug}
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

  return (
    <routes.assistants.detail.Link
      params={[assistant.id]}
      className="focus-visible:ring-ring block rounded-xl no-underline focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
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
            <MoreActions
              actions={[
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
              ]}
            />
          </div>
        </div>

        {/* Metadata: model + MCP servers */}
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-1.5">
            <Cpu className="text-muted-foreground/70 size-3.5 shrink-0" />
            <Type muted small className="truncate" title={assistant.model}>
              {assistant.model}
            </Type>
          </div>
          <AssistantToolsets assistant={assistant} />
        </div>

        {/* Footer row: status toggle + last updated */}
        <div className="border-border/60 mt-auto flex items-center justify-between gap-2 border-t pt-3">
          <StatusToggle assistant={assistant} />
          <UpdatedAt date={new Date(assistant.updatedAt)} />
        </div>
      </DotCard>
    </routes.assistants.detail.Link>
  );
}
