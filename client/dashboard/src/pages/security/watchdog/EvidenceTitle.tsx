import { Icon } from "@/components/ui/Icon";
import { useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { cn } from "@/lib/utils";
import {
  getMatchStrings,
  highlightMatches,
} from "@/pages/chatLogs/chatHelpers";
import { messageText } from "@/pages/chatLogs/transcript";
import { useRoutes } from "@/routes";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { useQuery } from "@tanstack/react-query";
import { formatDistanceToNow } from "date-fns";
import { useLayoutEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { REVEAL_SCOPE } from "../unmask";

/**
 * Evidence card header: the session title with a chevron and a session link
 * right after it, and the finding's age on the right. Clicking the title opens
 * the session transcript over the drawer, the link opens the same session in
 * Agent Sessions in a new tab, and the chevron swaps the title for the whole
 * message, in place. A prompt-derived title is cut to 80 runes when the chat
 * is saved, so the rest of the message never reaches this list and has to be
 * loaded: the full text is the session's opening user message. Findings with
 * no chat, and viewers without chat:read, only get the chevron when the title
 * visually overflows, and it just un-clips the title.
 */
export function EvidenceTitle({
  title,
  createdAt,
  chatId: findingChatId,
  onOpenChat,
}: {
  title: string;
  createdAt: Date;
  chatId: string | undefined;
  onOpenChat: (chatId: string) => void;
}): JSX.Element {
  const routes = useRoutes();
  const { hasScope } = useRBAC();
  // Everything chat-backed here (the transcript, the full message, the session
  // link) shows chat content, so it only exists for chat:read holders. Anyone
  // else gets the plain title, and no request for the chat is ever made.
  // Checked against this chat: grants are "All sessions" in the roles UI, but
  // the API accepts a per-chat selector, and an unscoped check would pass on
  // any chat:read grant at all.
  const chatId =
    findingChatId && hasScope(REVEAL_SCOPE, findingChatId)
      ? findingChatId
      : undefined;
  const [expanded, setExpanded] = useState(false);
  const [clipped, setClipped] = useState(false);
  const titleRef = useRef<HTMLElement>(null);
  // Only measured while collapsed: an expanded title never overflows, and the
  // chevron has to stay so it can collapse again.
  useLayoutEffect(() => {
    const el = titleRef.current;
    if (!el || expanded) return;
    setClipped(el.scrollWidth > el.clientWidth);
  }, [title, expanded]);
  const fullMessage = useFullMessage(chatId, expanded);
  const titleClassName = cn(
    "text-muted-foreground min-w-0 font-mono text-xs",
    expanded ? "break-words whitespace-pre-wrap" : "truncate",
  );
  return (
    <div className="flex items-start justify-between gap-4 px-3 py-2">
      <div className="flex min-w-0 items-start gap-1">
        {chatId ? (
          <button
            ref={titleRef as React.Ref<HTMLButtonElement>}
            type="button"
            title="Open session"
            className={cn(titleClassName, "hover:text-foreground text-left")}
            onClick={() => onOpenChat(chatId)}
          >
            {(expanded && fullMessage) || title}
          </button>
        ) : (
          <span ref={titleRef} className={titleClassName}>
            {title}
          </span>
        )}
        {(chatId || clipped) && (
          <button
            type="button"
            aria-expanded={expanded}
            aria-label={expanded ? "Collapse message" : "Show full message"}
            className="text-muted-foreground hover:text-foreground shrink-0"
            onClick={() => setExpanded((prev) => !prev)}
          >
            <Icon
              name={expanded ? "chevron-up" : "chevron-down"}
              className="size-4"
            />
          </button>
        )}
        {chatId && (
          <Link
            to={`${routes.agentSessions.href()}?${new URLSearchParams({ chatId })}`}
            target="_blank"
            rel="noopener noreferrer"
            aria-label="Open session in Agent Sessions"
            title="Open in Agent Sessions"
            className="text-muted-foreground hover:text-foreground shrink-0"
          >
            <Icon name="external-link" className="size-3.5" />
          </Link>
        )}
      </div>
      <span className="text-muted-foreground shrink-0 font-mono text-xs">
        {formatDistanceToNow(createdAt, { addSuffix: true })}
      </span>
    </div>
  );
}

// Enough to reach the first user turn past any system or developer preamble.
const OPENING_MESSAGE_LOOKAHEAD = 20;

/**
 * The session's opening user message, loaded once the header is expanded.
 * Every flagged match in it stays dotted out, as in the transcript: revealing
 * a match is an audited action this preview must not bypass. Returns null
 * while loading or when it can't be shown (no chat:read, or the findings
 * needed for masking failed to load or could not be fully paged), leaving the title in place.
 */
function useFullMessage(
  chatId: string | undefined,
  enabled: boolean,
): React.ReactNode {
  const client = useSdkClient();
  const active = enabled && Boolean(chatId);
  const messageQuery = useQuery({
    queryKey: ["chat", chatId, "opening-message"],
    queryFn: () =>
      client.chat.load({
        id: chatId ?? "",
        fromStart: true,
        limit: OPENING_MESSAGE_LOOKAHEAD,
      }),
    enabled: active,
    throwOnError: false,
  });
  // Every finding in the chat, not just the first page: a match the masking
  // pass never sees would print in the clear. A chat with more findings than
  // the page budget resolves to null, which keeps the message hidden.
  const findingsQuery = useQuery({
    queryKey: ["chat", chatId, "all-findings"],
    queryFn: () => collectChatFindings(client, chatId ?? ""),
    enabled: active,
    throwOnError: false,
  });
  if (!messageQuery.data || !findingsQuery.data) return null;
  const opening = messageQuery.data.messages.find((m) => m.role === "user");
  const text = opening ? messageText(opening.content) : "";
  if (!text) return null;
  return highlightMatches(
    text,
    getMatchStrings(findingsQuery.data),
    true,
    true,
  );
}

const FINDINGS_PAGE_SIZE = 200;
const FINDINGS_MAX_PAGES = 25;

/** Pages `risk.listResults` for one chat to the end. Returns null when the
 * chat has more findings than the page budget, so the caller can refuse to
 * render rather than mask from a partial set. */
async function collectChatFindings(
  client: ReturnType<typeof useSdkClient>,
  chatId: string,
): Promise<RiskResult[] | null> {
  const all: RiskResult[] = [];
  let cursor: string | undefined = undefined;
  for (let page = 0; page < FINDINGS_MAX_PAGES; page++) {
    const res = await client.risk.results.list({
      chatId,
      cursor,
      limit: FINDINGS_PAGE_SIZE,
    });
    all.push(...res.results);
    cursor = res.nextCursor ?? undefined;
    if (!cursor) return all;
  }
  return null;
}
