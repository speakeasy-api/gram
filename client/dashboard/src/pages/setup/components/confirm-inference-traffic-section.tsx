import { useMemo, useRef } from "react";
import { useListChats } from "@gram/client/react-query/listChats.js";
import { ExternalLink } from "lucide-react";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { StepSection } from "./step-section";
import {
  sourceLabel,
  useTrafficArrivals,
  type TrafficActivity,
} from "./traffic-activity";
import { TrafficActivityPanel, TrafficBadge } from "./traffic-activity-panel";

// Chats are heavier to list than the hook tail is to poll, and a conversation
// is a coarser unit than a tool call, so this checks less often.
const POLL_INTERVAL_MS = 5000;
const PAGE_SIZE = 8;

/**
 * The product surfaces an Anthropic inference hook delivers under. Claude
 * writes conversations from claude.ai and from Claude Code's web surface;
 * anything it cannot place lands under the generic `anthropic-inference`,
 * which names no surface and so confirms nothing an admin set up here.
 */
const INFERENCE_SOURCES = "claude-chat-web,claude-code-web";

// Claude Desktop registers the claude:// scheme, so this hands the reader the
// app rather than the website. Deliberately not target="_blank": a custom
// scheme opened in a new tab launches the app and leaves a blank tab behind.
// With the app not installed the click is inert, which is the same outcome as
// the reader not having Claude Desktop to test with.
const CLAUDE_DESKTOP_URL = "claude://";

interface ConfirmInferenceTrafficSectionProps {
  index: number;
  /** What the admin should do to make a conversation show up. */
  description: string;
}

// Inference hook deliveries never become hook events: Claude posts a whole
// conversation, which Speakeasy stores as chat messages rather than as the
// telemetry logs `ConfirmTrafficSection` reads. So this asks the same question
// of the same panel over the chat listing instead — a conversation last active
// since this card was opened is traffic the hook just delivered.
export function ConfirmInferenceTrafficSection({
  index,
  description,
}: ConfirmInferenceTrafficSectionProps): JSX.Element {
  // Only count conversations active after the admin opened this card. Unlike
  // the hook tail there is no cursor to advance: the same page is re-listed
  // every poll and the arrivals hook drops what it has already played.
  const sinceRef = useRef<Date>(new Date());

  const query = useListChats(
    {
      source: INFERENCE_SOURCES,
      from: sinceRef.current,
      sortBy: "last_message_timestamp",
      sortOrder: "desc",
      limit: PAGE_SIZE,
      offset: 0,
    },
    undefined,
    { refetchInterval: POLL_INTERVAL_MS, throwOnError: false, retry: false },
  );

  const chats = query.data?.chats;
  const incoming = useMemo<TrafficActivity[]>(
    () =>
      (chats ?? []).map((chat) => {
        const source = chat.source ?? "";
        const timeMs = chat.lastMessageTimestamp.getTime();
        return {
          // A conversation arrives again every time Claude delivers another
          // turn of it, so its last activity is part of its identity.
          key: `${chat.id}|${timeMs}`,
          source,
          actor: chat.accountEmail ?? chat.externalUserId ?? undefined,
          action: chat.title || `${sourceLabel(source)} conversation`,
          timeMs,
        };
      }),
    [chats],
  );

  const { events, hasEvents } = useTrafficArrivals(incoming);

  return (
    <StepSection
      index={index}
      slug="confirm-traffic"
      title="Confirm traffic"
      description={description}
      complete={hasEvents}
      aside={<TrafficBadge hasEvents={hasEvents} />}
    >
      <div className="space-y-4">
        <a
          href={CLAUDE_DESKTOP_URL}
          className="border-border bg-card hover:border-foreground/20 inline-flex items-center gap-2 border px-3 py-2 text-sm transition-colors"
        >
          <AgentProviderIcon
            source="claude"
            className="h-4 w-4 flex-shrink-0"
          />
          <span className="text-foreground">Open Claude</span>
          <ExternalLink className="text-muted-foreground h-3.5 w-3.5 flex-shrink-0" />
        </a>
        <TrafficActivityPanel
          index={index}
          events={events}
          hasEvents={hasEvents}
          pollIntervalMs={POLL_INTERVAL_MS}
          isError={query.isError}
          onRetry={() => void query.refetch()}
        />
      </div>
    </StepSection>
  );
}
