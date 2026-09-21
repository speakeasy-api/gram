import { Icon } from "@/components/ui/Icon";
import { useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { cn } from "@/lib/utils";
import {
  getMatchStrings,
  highlightMatches,
  matchRanges,
  resultsAreSensitive,
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
 * message the finding was flagged in, in place. The card otherwise shows only
 * the matched span, and the title is a session label cut to 80 runes, so the
 * message itself never reaches this list and has to be loaded. Findings with
 * no chat, and viewers without chat:read, only get the chevron when the title
 * visually overflows, and it just un-clips the title.
 */
export function EvidenceTitle({
  title,
  createdAt,
  chatId: findingChatId,
  chatMessageId,
  onOpenChat,
}: {
  title: string;
  createdAt: Date;
  chatId: string | undefined;
  /** The message the finding was flagged in; what the chevron expands to. */
  chatMessageId: string | undefined;
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
  const flaggedMessage = useFlaggedMessage(chatId, chatMessageId, expanded);
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
            {(expanded && flaggedMessage) || title}
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
            aria-label={expanded ? "Collapse message" : "Show flagged message"}
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

// The transcript's own risk-view request: windows of messages around every
// flagged one, which is the cheapest load guaranteed to include this finding's
// message for all but very long sessions.
const RISK_WINDOW_LIMIT = 200;

/**
 * The message this finding was flagged in, loaded once the header is expanded.
 * Flagged secrets in it stay dotted out, as in the transcript: revealing one
 * is an audited action this preview must not bypass. Returns null
 * while loading or when it can't be shown (the message is outside the first
 * risk window, or the findings needed for masking failed to load or could not
 * be fully paged), leaving the title in place.
 */
function useFlaggedMessage(
  chatId: string | undefined,
  chatMessageId: string | undefined,
  enabled: boolean,
): React.ReactNode {
  const client = useSdkClient();
  const active = enabled && Boolean(chatId) && Boolean(chatMessageId);
  const messageQuery = useQuery({
    queryKey: ["chat", chatId, "risk-window"],
    queryFn: () =>
      client.chat.load({
        id: chatId ?? "",
        riskOnly: true,
        limit: RISK_WINDOW_LIMIT,
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
  const flagged = messageQuery.data.messages.find(
    (m) => m.id === chatMessageId,
  );
  if (!flagged) return null;
  // A flagged tool call carries its payload in toolCalls, with no content.
  const text = messageText(flagged.content) || flagged.toolCalls || "";
  if (!text) return null;
  // The transcript's masking rule: literal secrets and PII (gitleaks, presidio)
  // are dotted out, other matches such as a flagged command are highlighted but
  // readable. Judged against every finding in the chat, so a secret flagged on
  // another message is still masked if it recurs in this one.
  const findings = findingsQuery.data;
  const secretMatches = getMatchStrings(
    findings.filter((r) => resultsAreSensitive([r])),
  );
  const masked = matchRanges(text, secretMatches).length > 0;
  return highlightMatches(text, getMatchStrings(findings), masked, masked);
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
