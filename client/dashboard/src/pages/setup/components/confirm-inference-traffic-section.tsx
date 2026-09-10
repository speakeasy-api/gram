import { useMemo, useRef } from "react";
import { useListChats } from "@gram/client/react-query/listChats.js";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { StepSection } from "./step-section";
import {
  sourceLabel,
  trafficBadge,
  TrafficActivityPanel,
  useTrafficArrivals,
  type TrafficActivity,
} from "./traffic-activity-panel";

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

interface ConfirmInferenceTrafficSectionProps {
  index: number;
  /** What the admin should do to make a conversation show up. */
  description: string;
  callout?: { title: string; body: string };
}

// Inference hook deliveries never become hook events: Claude posts a whole
// conversation, which Speakeasy stores as chat messages rather than as the
// telemetry logs `ConfirmTrafficSection` reads. So this asks the same question
// of the same panel over the chat listing instead — a conversation last active
// since this card was opened is traffic the hook just delivered.
export function ConfirmInferenceTrafficSection({
  index,
  description,
  callout,
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
        return {
          // A conversation arrives again every time Claude delivers another
          // turn of it, so its last activity is part of its identity.
          key: `${chat.id}|${chat.lastMessageTimestamp}`,
          source,
          actor: chat.accountEmail ?? chat.externalUserId ?? undefined,
          action: chat.title || `${sourceLabel(source)} conversation`,
          timeMs: new Date(chat.lastMessageTimestamp).getTime(),
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
      aside={trafficBadge(hasEvents)}
    >
      <div className="space-y-4">
        {callout ? (
          <Alert variant="info" alignTop>
            <AlertTitle>{callout.title}</AlertTitle>
            <AlertDescription>{callout.body}</AlertDescription>
          </Alert>
        ) : null}
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
