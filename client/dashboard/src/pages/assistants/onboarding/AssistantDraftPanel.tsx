import { AssistantOwner } from "@/components/assistants/assistant-owner";
import { AssistantSessionsList } from "@/components/assistants/sessions-list";
import { AssistantStatusToggle } from "@/components/assistants/status-toggle";
import { EditInstructionsDialog } from "@/components/assistants/edit-instructions-dialog";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  PageTabsList,
} from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { useAssistantsDeleteMutation } from "@gram/client/react-query/assistantsDelete.js";
import { invalidateAllAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { useTriggers } from "@gram/client/react-query/triggers.js";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { parseAsStringLiteral, useQueryState } from "nuqs";
import { useState } from "react";
import { AssistantSkillsSection } from "./AssistantSkillsSection";
import { useAssistantDraft } from "./useAssistantDraft";

const DETAIL_TABS = ["overview", "sessions", "triggers"] as const;
type DetailTab = (typeof DETAIL_TABS)[number];

function toDetailTab(value: string): DetailTab {
  return (DETAIL_TABS as readonly string[]).includes(value)
    ? (value as DetailTab)
    : "overview";
}

export function AssistantDraftPanel(): JSX.Element {
  const draft = useAssistantDraft();
  const routes = useRoutes();
  const project = useProject();
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useQueryState(
    "tab",
    parseAsStringLiteral(DETAIL_TABS).withDefault("overview"),
  );
  const [editingInstructions, setEditingInstructions] = useState(false);

  // Only fetch triggers when the Triggers tab is active — they are no longer
  // surfaced on the Overview tab, so loading the panel shouldn't pay for them.
  const { data: triggersData } = useTriggers(undefined, undefined, {
    retry: false,
    throwOnError: false,
    enabled: !!draft.assistantId && activeTab === "triggers",
  });

  const triggers = (triggersData?.triggers ?? []).filter(
    (t) => t.targetKind === "assistant" && t.targetRef === draft.assistantId,
  );

  const del = useAssistantsDeleteMutation({
    onSuccess: () => {
      void invalidateAllAssistantsList(queryClient);
      routes.assistants.goTo();
    },
  });

  if (!draft.assistantId) {
    return (
      <div className="flex h-full flex-col">
        <div className="border-border border-b px-4 py-3">
          <Text variant="body" className="font-medium">
            Draft assistant
          </Text>
        </div>
        <div className="flex flex-1 items-center justify-center px-4 py-8 text-center">
          <Stack gap={3} align="center" className="max-w-xs">
            <div className="bg-muted/40 flex h-10 w-10 items-center justify-center rounded-full">
              <Icon name="bot" className="text-muted-foreground h-5 w-5" />
            </div>
            <Text small muted>
              Once you describe your assistant in the chat, the live spec will
              appear here as it's built.
            </Text>
          </Stack>
        </div>
      </div>
    );
  }

  const a = draft.assistant;

  return (
    <div className="flex h-full flex-col">
      <div className="border-border flex items-center justify-between gap-2 border-b px-4 py-3">
        <Text variant="body" className="truncate font-medium">
          {a?.name ?? "Loading…"}
        </Text>
        <Button
          variant="tertiary"
          size="sm"
          className="shrink-0"
          aria-label="Delete assistant"
          onClick={() => {
            if (!draft.assistantId) return;
            if (!confirm("Delete this assistant? This cannot be undone."))
              return;
            del.mutate({ request: { id: draft.assistantId } });
          }}
          disabled={del.isPending}
        >
          {del.isPending ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <Icon name="trash" className="h-3 w-3" />
          )}
        </Button>
      </div>

      {!a ? (
        <Stack align="center" justify="center" className="flex-1 py-12">
          <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
        </Stack>
      ) : (
        <Tabs
          value={activeTab}
          onValueChange={(value) => void setActiveTab(toDetailTab(value))}
          className="flex min-h-0 flex-1 flex-col"
        >
          <div className="border-border border-b px-4">
            <PageTabsList className="h-auto gap-6 bg-transparent p-0">
              <PageTabsTrigger value="overview">Overview</PageTabsTrigger>
              <PageTabsTrigger value="sessions">Sessions</PageTabsTrigger>
              <PageTabsTrigger value="triggers">Triggers</PageTabsTrigger>
            </PageTabsList>
          </div>

          <TabsContent
            value="overview"
            className="min-h-0 flex-1 overflow-y-auto px-4 py-4"
          >
            <Stack gap={5}>
              <Section title="Overview">
                <Row label="Status">
                  <AssistantStatusToggle
                    assistant={a}
                    onUpdated={() => void draft.refetchAssistant()}
                  />
                </Row>
                <Row label="Model">
                  <code className="text-xs">{a.model}</code>
                </Row>
                <Row label="Owner">
                  <AssistantOwner
                    createdByUserId={a.createdByUserId}
                    variant="row"
                  />
                </Row>
                <Row label="Concurrency">
                  <Text small>{a.maxConcurrency}</Text>
                </Row>
                <Row label="Warm TTL">
                  <Text small>{a.warmTtlSeconds}s</Text>
                </Row>
              </Section>

              <Section
                title="System instructions"
                action={
                  <Button
                    variant="tertiary"
                    size="sm"
                    className="h-auto gap-1 px-1.5 py-0.5 text-xs"
                    onClick={() => setEditingInstructions(true)}
                  >
                    <Icon name="pencil" className="h-3 w-3" />
                    {a.instructions ? "Expand & edit" : "Add"}
                  </Button>
                }
              >
                {a.instructions ? (
                  <button
                    type="button"
                    onClick={() => setEditingInstructions(true)}
                    className="hover:border-border block w-full border border-transparent text-left"
                  >
                    <pre className="bg-muted/30 max-h-48 overflow-y-auto p-3 font-mono text-[11px] whitespace-pre-wrap">
                      {a.instructions}
                    </pre>
                  </button>
                ) : (
                  <Text small muted>
                    Not set yet.
                  </Text>
                )}
              </Section>

              <RequireScope
                scope="skill:read"
                resourceId={project.id}
                level="section"
              >
                <AssistantSkillsSection />
              </RequireScope>

              <Section
                title={`MCP Servers (${
                  a.toolsets.length + (a.mcpServers ?? []).length
                })`}
                empty="No MCP servers attached."
                isEmpty={
                  a.toolsets.length === 0 && (a.mcpServers ?? []).length === 0
                }
              >
                <Stack gap={2}>
                  {a.toolsets.map((t) => (
                    <routes.mcp.details.Link
                      key={t.toolsetSlug}
                      params={[t.toolsetSlug]}
                      className="border-border hover:bg-surface-secondary flex items-center justify-between border px-3 py-2 transition-colors hover:no-underline"
                    >
                      <Stack gap={0} className="min-w-0">
                        <code className="truncate text-xs">
                          {t.toolsetSlug}
                        </code>
                        {t.environmentSlug && (
                          <Text small muted className="text-[11px]">
                            env: {t.environmentSlug}
                          </Text>
                        )}
                      </Stack>
                      <Icon
                        name="chevron-right"
                        className="text-muted-foreground h-4 w-4 shrink-0"
                      />
                    </routes.mcp.details.Link>
                  ))}
                  {(a.mcpServers ?? []).map((m) => (
                    <routes.mcp.x.Link
                      key={m.mcpServerSlug}
                      params={[m.mcpServerSlug]}
                      className="border-border hover:bg-surface-secondary flex items-center justify-between border px-3 py-2 transition-colors hover:no-underline"
                    >
                      <Stack gap={0} className="min-w-0">
                        <code className="truncate text-xs">
                          {m.mcpServerSlug}
                        </code>
                        {m.environmentSlug && (
                          <Text small muted className="text-[11px]">
                            env: {m.environmentSlug}
                          </Text>
                        )}
                      </Stack>
                      <Icon
                        name="chevron-right"
                        className="text-muted-foreground h-4 w-4 shrink-0"
                      />
                    </routes.mcp.x.Link>
                  ))}
                </Stack>
              </Section>
            </Stack>
          </TabsContent>

          <TabsContent
            value="sessions"
            className="min-h-0 flex-1 overflow-y-auto px-4 py-4"
          >
            <AssistantSessionsList assistantId={a.id} />
          </TabsContent>

          <TabsContent
            value="triggers"
            className="min-h-0 flex-1 overflow-y-auto px-4 py-4"
          >
            {triggers.length === 0 ? (
              <Text small muted>
                No triggers wired up.
              </Text>
            ) : (
              <Stack gap={2}>
                {triggers.map((t) => (
                  <div
                    key={t.id}
                    className="border-border flex items-start justify-between gap-2 border px-3 py-2"
                  >
                    <Stack gap={1} className="min-w-0">
                      <Stack direction="horizontal" gap={2} align="center">
                        <Text small className="font-medium">
                          {t.name}
                        </Text>
                        <Badge variant="neutral" className="text-[10px]">
                          {t.definitionSlug}
                        </Badge>
                      </Stack>
                      {t.webhookUrl && (
                        <code className="text-muted-foreground truncate text-[10px]">
                          {t.webhookUrl}
                        </code>
                      )}
                    </Stack>
                    {t.status === "active" ? (
                      <Badge variant="neutral" className="text-[10px]">
                        Active
                      </Badge>
                    ) : (
                      <Badge variant="neutral" className="text-[10px]">
                        Paused
                      </Badge>
                    )}
                  </div>
                ))}
              </Stack>
            )}
          </TabsContent>
        </Tabs>
      )}

      {a && (
        <EditInstructionsDialog
          assistant={a}
          open={editingInstructions}
          onOpenChange={setEditingInstructions}
          onUpdated={() => void draft.refetchAssistant()}
        />
      )}
    </div>
  );
}

function Section({
  title,
  children,
  empty,
  isEmpty,
  action,
}: {
  title: string;
  children: React.ReactNode;
  empty?: string;
  isEmpty?: boolean;
  action?: React.ReactNode;
}) {
  return (
    <div>
      <div className="mb-2 flex items-center justify-between gap-2">
        <Text variant="body" className="text-xs font-semibold uppercase">
          {title}
        </Text>
        {action}
      </div>
      {isEmpty && empty ? (
        <Text small muted>
          {empty}
        </Text>
      ) : (
        children
      )}
    </div>
  );
}

function Row({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center justify-between py-1">
      <Text small muted>
        {label}
      </Text>
      <div>{children}</div>
    </div>
  );
}
