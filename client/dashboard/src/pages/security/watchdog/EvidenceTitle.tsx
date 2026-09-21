import { Icon } from "@/components/ui/Icon";
import { useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { cn } from "@/lib/utils";
import { agentSessionHref } from "@/pages/chatLogs/agentSessionLink";
import {
  getMatchStrings,
  highlightMatches,
  matchRanges,
  resultsAreSensitive,
} from "@/pages/chatLogs/chatHelpers";
import { parseToolCalls } from "@/pages/chatLogs/traceEntries";
import { argsToString, messageText } from "@/pages/chatLogs/transcript";
import { WINDOW_INITIAL_LIMIT } from "@/pages/chatLogs/useWindowedTranscript";
import { useRoutes } from "@/routes";
import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import { useLoadChat } from "@gram/client/react-query/loadChat.js";
import { useQuery } from "@tanstack/react-query";
import { formatDistanceToNow } from "date-fns";
import { Loader2 } from "lucide-react";
import { useLayoutEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { REVEAL_SCOPE } from "../unmask";
import { collectChatFindings } from "./collect-findings";

/**
 * Evidence card header: the session title with a chevron and a session link
 * right after it, and the finding's age on the right. Clicking the title opens
 * the session transcript over the drawer, the link opens the same session in
 * Agent Sessions in a new tab, and the chevron shows the whole message the
 * finding was flagged in under the title. The card otherwise shows only
 * the matched span, and the title is a session label cut to 80 runes, so the
 * message itself never reaches this list and has to be loaded. A message that
 * turns out not to be showable says why under the title, which stays in place.
 * Findings with no chat or message, and viewers without chat:read, only get
 * the chevron when the title visually overflows, and it just un-clips the
 * title.
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
  onOpenChat: (chatId: string, chatMessageId?: string) => void;
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
  const expandsToMessage = Boolean(chatId) && Boolean(chatMessageId);
  const titleClassName = cn(
    "text-muted-foreground min-w-0 font-mono text-xs",
    expanded ? "break-words whitespace-pre-wrap" : "truncate",
  );
  return (
    <div className="flex items-start justify-between gap-4 px-3 py-2">
      <div className="flex min-w-0 flex-col gap-1">
        <div className="flex min-w-0 items-start gap-1">
          {chatId ? (
            <button
              ref={titleRef as React.Ref<HTMLButtonElement>}
              type="button"
              title="Open session"
              className={cn(titleClassName, "hover:text-foreground text-left")}
              onClick={() => onOpenChat(chatId, chatMessageId)}
            >
              {title}
            </button>
          ) : (
            <span ref={titleRef} className={titleClassName}>
              {title}
            </span>
          )}
          {(expandsToMessage || clipped) && (
            <button
              type="button"
              aria-expanded={expanded}
              aria-label={
                expanded
                  ? "Collapse message"
                  : expandsToMessage
                    ? "Show flagged message"
                    : "Show full title"
              }
              className="text-muted-foreground hover:text-foreground shrink-0"
              onClick={() => setExpanded((prev) => !prev)}
            >
              {flaggedMessage.loading ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Icon
                  name={expanded ? "chevron-up" : "chevron-down"}
                  className="size-4"
                />
              )}
            </button>
          )}
          {chatId && (
            <Link
              to={agentSessionHref(routes.agentSessions.href(), chatId)}
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
        {/* Outside the title button so the message can be selected and
            copied, and a click on it doesn't open the session. */}
        {expanded && flaggedMessage.content && (
          <div className="text-muted-foreground font-mono text-xs break-words whitespace-pre-wrap">
            {flaggedMessage.content}
          </div>
        )}
        {expanded && flaggedMessage.problem && (
          <p
            role="alert"
            className={cn(
              "text-xs",
              flaggedMessage.problem.failed
                ? "text-destructive"
                : "text-warning",
            )}
          >
            {flaggedMessage.problem.text}
          </p>
        )}
      </div>
      <span className="text-muted-foreground shrink-0 font-mono text-xs">
        {formatDistanceToNow(createdAt, { addSuffix: true })}
      </span>
    </div>
  );
}

/**
 * The message this finding was flagged in, loaded once the header is expanded.
 * Flagged secrets in it stay dotted out, as in the transcript: revealing one
 * is an audited action this preview must not bypass. `problem` is set once the
 * load settles without a showable message and says why: a request failed
 * (`failed`), or, as warnings, the message is not in the chat's latest
 * generation, the finding is no longer active, or the findings needed for
 * masking could not be fully paged.
 */
function useFlaggedMessage(
  chatId: string | undefined,
  chatMessageId: string | undefined,
  enabled: boolean,
): {
  content: React.ReactNode;
  loading: boolean;
  problem: { text: string; failed: boolean } | null;
} {
  const client = useSdkClient();
  const active = enabled && Boolean(chatId) && Boolean(chatMessageId);
  // The risk transcript's own first request (every actively flagged message in
  // the latest generation, with context around each), so opening the session
  // afterwards reuses this response. A finding carries no generation, so one
  // flagged before the chat was compacted or edited can't be asked for.
  const messageQuery = useLoadChat(
    { id: chatId ?? "", limit: WINDOW_INITIAL_LIMIT, riskOnly: true },
    undefined,
    { enabled: active, throwOnError: false },
  );
  // Every finding in the chat, not just the first page: a match the masking
  // pass never sees would print in the clear. A chat with more findings than
  // the page budget resolves to null, which keeps the message hidden.
  const findingsQuery = useQuery({
    queryKey: ["chat", chatId, "all-findings"],
    queryFn: () => collectChatFindings(client, chatId ?? ""),
    enabled: active,
    throwOnError: false,
  });
  const none = { content: null, loading: false, problem: null };
  if (!active) return none;
  if (messageQuery.isPending || findingsQuery.isPending) {
    return { ...none, loading: true };
  }
  if (messageQuery.isError || findingsQuery.isError || !messageQuery.data) {
    return {
      ...none,
      problem: { text: "Couldn't load this message. Try again.", failed: true },
    };
  }
  if (!findingsQuery.data) {
    return {
      ...none,
      problem: {
        text: "This session has too many findings to mask the message here. Open the session to read it.",
        failed: false,
      },
    };
  }
  const flagged = messageQuery.data.messages.find(
    (m) => m.id === chatMessageId,
  );
  const text = flagged ? flaggedMessageText(flagged) : "";
  if (!text) {
    return {
      ...none,
      problem: {
        text:
          messageQuery.data.maxGeneration > 0
            ? "This message is from an earlier version of the session, before it was compacted or edited, and can't be shown here."
            : "This message is no longer flagged in the session, so it can't be shown here.",
        failed: false,
      },
    };
  }
  // The transcript's masking rule: once the message holds a literal secret or
  // PII (gitleaks, presidio), every match in it is dotted out; otherwise
  // matches such as a flagged command are highlighted but readable. Judged
  // against every finding in the chat, so a secret flagged on
  // another message is still masked if it recurs in this one.
  const findings = findingsQuery.data;
  const secretMatches = withJsonEscaped(
    getMatchStrings(findings.filter((r) => resultsAreSensitive([r]))),
  );
  const masked = matchRanges(text, secretMatches).length > 0;
  return {
    content: highlightMatches(
      text,
      withJsonEscaped(getMatchStrings(findings)),
      masked,
      masked,
    ),
    loading: false,
    problem: null,
  };
}

/** The message as the transcript words it: its text, then each tool call as
 * its name and arguments. The raw toolCalls string is never printed, because
 * arguments nested in it are escaped twice and a match would not line up. A
 * payload that can't be parsed contributes nothing. */
function flaggedMessageText(message: ChatMessage): string {
  const parts = [messageText(message.content)];
  for (const call of parseToolCalls(message.toolCalls) ?? []) {
    const name = call.function?.name ?? call.name;
    const args = argsToString(call.function?.arguments);
    parts.push([name, args].filter(Boolean).join("\n"));
  }
  return parts.filter(Boolean).join("\n\n");
}

/** Adds each match as it reads inside a JSON string, longest first. Tool call
 * arguments are JSON, so a secret holding a quote, backslash or newline
 * appears there escaped and the literal match alone would leave it unmasked. */
function withJsonEscaped(matches: string[]): string[] {
  const all = new Set(matches);
  for (const match of matches) all.add(JSON.stringify(match).slice(1, -1));
  return [...all].sort((a, b) => b.length - a.length);
}
