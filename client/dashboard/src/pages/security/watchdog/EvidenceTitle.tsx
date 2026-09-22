import { Icon } from "@/components/ui/Icon";
import { useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { cn } from "@/lib/utils";
import { agentSessionHref } from "@/pages/chatLogs/agentSessionLink";
import {
  getMatchStrings,
  highlightMatches,
  jsonEscaped,
  matchRanges,
  resultsAreSensitive,
  withJsonEscaped,
} from "@/pages/chatLogs/chatHelpers";
import { parseToolCalls } from "@/pages/chatLogs/traceEntries";
import {
  argsToString,
  messageEnvelope,
  messageText,
} from "@/pages/chatLogs/transcript";
import { WINDOW_INITIAL_LIMIT } from "@/pages/chatLogs/useWindowedTranscript";
import { useRoutes } from "@/routes";
import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { useLoadChat } from "@gram/client/react-query/loadChat.js";
import { useQuery } from "@tanstack/react-query";
import { formatDistanceToNow } from "date-fns";
import { Loader2 } from "lucide-react";
import { useLayoutEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { REVEAL_SCOPE } from "../unmask";
import { collectChatFindings } from "./collect-findings";

/**
 * Evidence card header. The title opens the session transcript, the chevron
 * loads and shows the flagged message (or says why it can't be shown), and the
 * link opens the session in Agent Sessions. Without a chat, a message, or
 * chat:read, the chevron only appears for a clipped title and un-clips it.
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
  // Chat content needs chat:read on this chat. An unscoped check would pass on
  // a grant for any chat. Without it, the chat is never requested.
  const chatId =
    findingChatId && hasScope(REVEAL_SCOPE, findingChatId)
      ? findingChatId
      : undefined;
  const [expanded, setExpanded] = useState(false);
  const [clipped, setClipped] = useState(false);
  const titleRef = useRef<HTMLElement>(null);
  // Only measured while collapsed, so the chevron stays to collapse again.
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
        {/* Outside the title button so the text can be selected. */}
        {expanded && (
          <FlaggedMessageBody message={flaggedMessage} chatId={chatId} />
        )}
      </div>
      <span className="text-muted-foreground shrink-0 font-mono text-xs">
        {formatDistanceToNow(createdAt, { addSuffix: true })}
      </span>
    </div>
  );
}

/** One finding's flagged message, loaded while `enabled`, or why it can't be shown. */
export function FlaggedMessage({
  chatId: findingChatId,
  chatMessageId,
  enabled,
}: {
  chatId: string | undefined;
  chatMessageId: string | undefined;
  enabled: boolean;
}): JSX.Element {
  const { hasScope } = useRBAC();
  // Same check as the header: chat:read on this chat, or no request at all.
  const chatId =
    findingChatId && hasScope(REVEAL_SCOPE, findingChatId)
      ? findingChatId
      : undefined;
  const message = useFlaggedMessage(chatId, chatMessageId, enabled);
  if (!findingChatId || !chatMessageId) {
    return (
      <p className="text-muted-foreground text-xs">
        This finding isn&apos;t tied to a single message.
      </p>
    );
  }
  if (!chatId) {
    return (
      <p className="text-muted-foreground text-xs">
        Viewing the flagged message needs chat:read on this session.
      </p>
    );
  }
  if (message.loading) {
    return <Loader2 className="text-muted-foreground size-4 animate-spin" />;
  }
  return <FlaggedMessageBody message={message} chatId={chatId} />;
}

function FlaggedMessageBody({
  message,
  chatId,
}: {
  message: ReturnType<typeof useFlaggedMessage>;
  chatId: string | undefined;
}): JSX.Element {
  const routes = useRoutes();
  return (
    <>
      {message.content && (
        <div className="text-muted-foreground font-mono text-xs break-words whitespace-pre-wrap">
          {message.content}
        </div>
      )}
      {message.problem && (
        <p
          role="alert"
          className={cn(
            "text-xs",
            message.problem.failed ? "text-destructive" : "text-warning",
          )}
        >
          {message.problem.text}
          {message.problem.linkToSession && chatId && (
            <>
              {" "}
              <Link
                to={agentSessionHref(routes.agentSessions.href(), chatId)}
                target="_blank"
                rel="noopener noreferrer"
                className="underline underline-offset-2"
              >
                View the full session
              </Link>
            </>
          )}
        </p>
      )}
    </>
  );
}

/**
 * The flagged message, loaded once the header is expanded. Secrets stay masked:
 * revealing one is an audited action this preview must not bypass. `problem`
 * says why a message can't be shown; `failed` marks a request error, and
 * `linkToSession` follows the text with a link to the session.
 */
function useFlaggedMessage(
  chatId: string | undefined,
  chatMessageId: string | undefined,
  enabled: boolean,
): {
  content: React.ReactNode;
  loading: boolean;
  problem: { text: string; failed: boolean; linkToSession?: boolean } | null;
} {
  const client = useSdkClient();
  const active = enabled && Boolean(chatId) && Boolean(chatMessageId);
  // Same request as the risk transcript, so opening the session reuses it.
  // Only covers the latest generation: a finding carries no generation.
  const messageQuery = useLoadChat(
    { id: chatId ?? "", limit: WINDOW_INITIAL_LIMIT, riskOnly: true },
    undefined,
    { enabled: active, throwOnError: false },
  );
  // Every finding in the chat, since an unseen match would print in the clear.
  // Null when the chat has too many to collect. Keyed under ["risk", "results"]
  // so dismissing or restoring a finding refetches it.
  const findingsQuery = useQuery({
    queryKey: ["risk", "results", "chat", chatId],
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
        // A finding carries no generation, so an older one can't be requested.
        text:
          messageQuery.data.maxGeneration > 0
            ? "This message is no longer available."
            : "This message is no longer flagged in the session, so it can't be shown here.",
        failed: false,
        linkToSession: messageQuery.data.maxGeneration > 0,
      },
    };
  }
  const findings = findingsQuery.data;
  if (flagged && hasUnlocatedSecret(findings, flagged, text)) {
    return {
      ...none,
      problem: {
        text: "A secret flagged in this message can't be located in its text, so it can't be masked here. Open the session to read it.",
        failed: false,
      },
    };
  }
  // The transcript's rule: a message holding a secret or PII has every match
  // masked; otherwise matches are highlighted but readable.
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

/** The message text, then each tool call's name and arguments. The raw
 * toolCalls string is double-escaped, so matches would not line up in it. */
function flaggedMessageText(message: ChatMessage): string {
  const parts = [messageText(message.content)];
  for (const call of parseToolCalls(message.toolCalls) ?? []) {
    const name = call.function?.name ?? call.name;
    const args = canonicalArgs(call.function?.arguments);
    parts.push([name, args].filter(Boolean).join("\n"));
  }
  return parts.filter(Boolean).join("\n\n");
}

/** Tool call arguments re-serialized, so a provider's own escaping (`\u00e9`,
 * `\/`) becomes the form `jsonEscaped` produces and every finding in the chat
 * lines up. Arguments that don't parse can't be matched, so they are left out. */
function canonicalArgs(args: string | object | undefined): string | undefined {
  if (typeof args !== "string") return argsToString(args);
  if (!args.trim()) return undefined;
  try {
    return argsToString(JSON.parse(args));
  } catch {
    // Printed as written, a secret flagged elsewhere in the chat could sit here
    // in an escaping no finding matches, and show unmasked.
    return "(arguments can't be parsed, so they aren't shown)";
  }
}

/** Whether a secret flagged on this message can't be found in `text`, e.g. in
 * arguments that were left out, so the message would read as if it had none.
 * A match in the stripped harness envelope is never shown, so it's fine. */
function hasUnlocatedSecret(
  findings: RiskResult[],
  message: ChatMessage,
  text: string,
): boolean {
  const envelope = messageEnvelope(message.content);
  const onMessage = findings.filter(
    (r) => r.chatMessageId === message.id && resultsAreSensitive([r]),
  );
  return getMatchStrings(onMessage).some(
    (match) =>
      !text.includes(match) &&
      !text.includes(jsonEscaped(match)) &&
      !envelope.includes(match),
  );
}
