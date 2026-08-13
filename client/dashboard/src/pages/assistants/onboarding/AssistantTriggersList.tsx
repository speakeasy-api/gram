import { Badge } from "@/components/ui/Badge";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { type BadgeVariant } from "@/components/ui/lib/types";
import { useChatDetailSheet } from "@/pages/chatLogs/useChatDetailSheet";
import {
  TriggerEvent,
  TriggerEventStatus,
} from "@gram/client/models/components/triggerevent.js";
import { TriggerInstance } from "@gram/client/models/components/triggerinstance.js";
import { useTriggerEvents } from "@gram/client/react-query/triggerEvents.js";
import { format } from "date-fns";
import { Loader2 } from "lucide-react";
import { useState } from "react";

function eventStatusVariant(status: TriggerEventStatus): BadgeVariant {
  switch (status) {
    case TriggerEventStatus.Completed:
      return "success";
    case TriggerEventStatus.Failed:
      return "destructive";
    case TriggerEventStatus.Processing:
      return "information";
    case TriggerEventStatus.Pending:
      return "neutral";
  }
}

/**
 * The assistant detail panel's Triggers tab: each trigger expands in place to
 * show its recent traffic — one row per dispatched event, linking to the
 * conversation the event was routed to.
 */
export function AssistantTriggersList({
  triggers,
}: {
  triggers: TriggerInstance[];
}): JSX.Element {
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const { openChat, sheet } = useChatDetailSheet();

  if (triggers.length === 0) {
    return (
      <Text small muted>
        No triggers wired up.
      </Text>
    );
  }

  return (
    <>
      <Stack gap={2}>
        {triggers.map((trigger) => (
          <TriggerRow
            key={trigger.id}
            trigger={trigger}
            expanded={expandedId === trigger.id}
            onToggle={() =>
              setExpandedId(expandedId === trigger.id ? null : trigger.id)
            }
            onOpenChat={openChat}
          />
        ))}
      </Stack>

      {sheet}
    </>
  );
}

function TriggerRow({
  trigger,
  expanded,
  onToggle,
  onOpenChat,
}: {
  trigger: TriggerInstance;
  expanded: boolean;
  onToggle: () => void;
  onOpenChat: (chatId: string) => void;
}): JSX.Element {
  return (
    <div className="border-border border">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        className="hover:bg-muted/50 flex w-full items-start justify-between gap-2 px-3 py-2 text-left transition-colors"
      >
        <Stack gap={1} className="min-w-0">
          <Stack direction="horizontal" gap={2} align="center">
            <Icon
              name={expanded ? "chevron-down" : "chevron-right"}
              className="text-muted-foreground h-3 w-3 shrink-0"
            />
            <Text small className="font-medium">
              {trigger.name}
            </Text>
            <Badge size="sm" variant="neutral">
              {trigger.definitionSlug}
            </Badge>
          </Stack>
          {trigger.webhookUrl && (
            <code className="text-muted-foreground truncate pl-5 text-[10px]">
              {trigger.webhookUrl}
            </code>
          )}
        </Stack>
        <Badge size="sm" variant="neutral">
          {trigger.status === "active" ? "Active" : "Paused"}
        </Badge>
      </button>

      {expanded && (
        <TriggerTraffic triggerId={trigger.id} onOpenChat={onOpenChat} />
      )}
    </div>
  );
}

function TriggerTraffic({
  triggerId,
  onOpenChat,
}: {
  triggerId: string;
  onOpenChat: (chatId: string) => void;
}): JSX.Element {
  const { data, isLoading, error } = useTriggerEvents(
    { id: triggerId },
    undefined,
    { retry: false, throwOnError: false, staleTime: 30_000 },
  );

  const events = data?.events ?? [];

  if (isLoading) {
    return (
      <Stack
        align="center"
        justify="center"
        className="border-border border-t py-4"
      >
        <Loader2 className="text-muted-foreground h-4 w-4 animate-spin" />
      </Stack>
    );
  }

  if (error) {
    return (
      <div className="border-border border-t px-3 py-2">
        <Text small muted>
          Couldn't load trigger traffic. {error.message}
        </Text>
      </div>
    );
  }

  if (events.length === 0) {
    return (
      <div className="border-border border-t px-3 py-2">
        <Text small muted>
          No traffic yet. Incoming events for this trigger will appear here.
        </Text>
      </div>
    );
  }

  return (
    <div className="divide-border/60 border-border border-t divide-y">
      {events.map((event) => (
        <TriggerEventRow key={event.id} event={event} onOpenChat={onOpenChat} />
      ))}
    </div>
  );
}

function TriggerEventRow({
  event,
  onOpenChat,
}: {
  event: TriggerEvent;
  onOpenChat: (chatId: string) => void;
}): JSX.Element {
  const chatId = event.chatId;
  return (
    <div className="flex items-start justify-between gap-2 px-3 py-2">
      <Stack gap={1} className="min-w-0">
        <div className="text-muted-foreground flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px]">
          <Badge size="sm" variant={eventStatusVariant(event.status)}>
            {event.status}
          </Badge>
          <span>{format(event.createdAt, "MMM d, HH:mm:ss")}</span>
          {event.attempts > 1 && (
            <>
              <span className="text-muted-foreground/40">·</span>
              <span>{event.attempts} attempts</span>
            </>
          )}
        </div>
        {event.lastError && (
          <Text small className="text-destructive line-clamp-2 text-[11px]">
            {event.lastError}
          </Text>
        )}
      </Stack>
      {chatId && (
        <button
          type="button"
          onClick={() => onOpenChat(chatId)}
          className="text-muted-foreground hover:text-foreground inline-flex shrink-0 items-center gap-1 text-[11px] transition-colors"
        >
          View conversation
          <Icon name="chevron-right" className="h-3 w-3" />
        </button>
      )}
    </div>
  );
}
