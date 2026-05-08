import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import {
  invalidateAllAssistantsList,
  useAssistantsDeleteMutation,
  useTriggers,
} from "@gram/client/react-query/index.js";
import { Icon, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useAssistantDraft } from "./useAssistantDraft";

export function AssistantDraftPanel() {
  const draft = useAssistantDraft();
  const routes = useRoutes();
  const queryClient = useQueryClient();

  const { data: triggersData } = useTriggers(undefined, undefined, {
    retry: false,
    throwOnError: false,
    enabled: !!draft.assistantId,
  });

  const triggers = (triggersData?.triggers ?? []).filter(
    (t) => t.targetKind === "assistant" && t.targetRef === draft.assistantId,
  );

  const del = useAssistantsDeleteMutation({
    onSuccess: () => {
      invalidateAllAssistantsList(queryClient);
      routes.assistants.goTo();
    },
  });

  if (!draft.assistantId) {
    return (
      <div className="flex h-full flex-col">
        <div className="border-border border-b px-4 py-3">
          <Type variant="body" className="font-medium">
            Draft assistant
          </Type>
        </div>
        <div className="flex flex-1 items-center justify-center px-4 py-8 text-center">
          <Stack gap={3} align="center" className="max-w-xs">
            <div className="bg-muted/40 flex h-10 w-10 items-center justify-center rounded-full">
              <Icon name="bot" className="text-muted-foreground h-5 w-5" />
            </div>
            <Type small muted>
              Once you describe your assistant in the chat, the live spec will
              appear here as it's built.
            </Type>
          </Stack>
        </div>
      </div>
    );
  }

  const a = draft.assistant;

  return (
    <div className="flex h-full flex-col">
      <div className="border-border flex items-center justify-between border-b px-4 py-3">
        <Type variant="body" className="font-medium">
          {a?.name ?? "Loading…"}
        </Type>
        <Button
          variant="ghost"
          size="sm"
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

      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
        {!a ? (
          <Stack align="center" justify="center" className="py-12">
            <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
          </Stack>
        ) : (
          <Stack gap={5}>
            <Section title="Overview">
              <Row label="Status">
                {a.status === "active" ? (
                  <Badge variant="default">Active</Badge>
                ) : (
                  <Badge variant="secondary">Paused</Badge>
                )}
              </Row>
              <Row label="Model">
                <code className="text-xs">{a.model}</code>
              </Row>
              <Row label="Concurrency">
                <Type small>{a.maxConcurrency}</Type>
              </Row>
              <Row label="Warm TTL">
                <Type small>{a.warmTtlSeconds}s</Type>
              </Row>
            </Section>

            <Section title="System instructions">
              {a.instructions ? (
                <pre className="bg-muted/30 max-h-48 overflow-y-auto rounded-md p-3 font-mono text-[11px] whitespace-pre-wrap">
                  {a.instructions}
                </pre>
              ) : (
                <Type small muted>
                  Not set yet.
                </Type>
              )}
            </Section>

            <Section
              title={`MCP Servers (${a.toolsets.length})`}
              empty="No MCP servers attached."
              isEmpty={a.toolsets.length === 0}
            >
              <Stack gap={2}>
                {a.toolsets.map((t) => (
                  <div
                    key={t.toolsetSlug}
                    className="border-border flex items-center justify-between rounded-md border px-3 py-2"
                  >
                    <Stack gap={0}>
                      <code className="text-xs">{t.toolsetSlug}</code>
                      {t.environmentSlug && (
                        <Type small muted className="text-[11px]">
                          env: {t.environmentSlug}
                        </Type>
                      )}
                    </Stack>
                  </div>
                ))}
              </Stack>
            </Section>

            <Section
              title={`Triggers (${triggers.length})`}
              empty="No triggers wired up."
              isEmpty={triggers.length === 0}
            >
              <Stack gap={2}>
                {triggers.map((t) => (
                  <div
                    key={t.id}
                    className="border-border flex items-start justify-between gap-2 rounded-md border px-3 py-2"
                  >
                    <Stack gap={1} className="min-w-0">
                      <Stack direction="horizontal" gap={2} align="center">
                        <Type small className="font-medium">
                          {t.name}
                        </Type>
                        <Badge variant="outline" className="text-[10px]">
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
                      <Badge variant="default" className="text-[10px]">
                        Active
                      </Badge>
                    ) : (
                      <Badge variant="secondary" className="text-[10px]">
                        Paused
                      </Badge>
                    )}
                  </div>
                ))}
              </Stack>
            </Section>
          </Stack>
        )}
      </div>
    </div>
  );
}

function Section({
  title,
  children,
  empty,
  isEmpty,
}: {
  title: string;
  children: React.ReactNode;
  empty?: string;
  isEmpty?: boolean;
}) {
  return (
    <div>
      <Type variant="body" className="mb-2 text-xs font-semibold uppercase">
        {title}
      </Type>
      {isEmpty && empty ? (
        <Type small muted>
          {empty}
        </Type>
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
      <Type small muted>
        {label}
      </Type>
      <div>{children}</div>
    </div>
  );
}
