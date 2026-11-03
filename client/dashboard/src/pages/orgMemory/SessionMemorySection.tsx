import { Button } from "@/components/ui/Button";
import { useRoutes } from "@/routes";
import { invalidateAllListChats } from "@gram/client/react-query/listChats.js";
import {
  setChatMemoryData,
  useChatMemory,
} from "@gram/client/react-query/chatMemory.js";
import { useChatSetMemoryExcludedMutation } from "@gram/client/react-query/chatSetMemoryExcluded.js";
import { invalidateAllOrgMemoryDigests } from "@gram/client/react-query/orgMemoryDigests.js";
import { invalidateAllOrgMemoryOverview } from "@gram/client/react-query/orgMemoryOverview.js";
import { invalidateAllOrgMemoryRecent } from "@gram/client/react-query/orgMemoryRecent.js";
import { useQueryClient } from "@tanstack/react-query";
import { format, formatDistanceToNow } from "date-fns";
import { Brain, ChevronDown, ChevronUp } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { MemoryStateBadge } from "./MemoryStateBadge";
import { formatPeriod } from "./memoryFormat";

/**
 * The session sheet's view of Organizational Memory: whether agents can
 * recall this session, the masked summary they read, memos saved from it,
 * the day digest that folded it, and a switch to keep it out.
 */
export function SessionMemorySection({
  chatId,
}: {
  chatId: string;
}): JSX.Element | null {
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const [expanded, setExpanded] = useState(false);
  const memory = useChatMemory({ id: chatId }, undefined, {
    throwOnError: false,
    retry: false,
  });
  const setExcluded = useChatSetMemoryExcludedMutation();

  // Memory is off for the organization (404), or the caller can't read
  // memory state: say nothing. Nothing shows until the server has answered,
  // so organizations without memory never see the section flash.
  if (memory.isError || memory.isPending) return null;

  const data = memory.data;
  const state = data?.state;
  const excluded = state === "excluded";
  const canToggle = data
    ? excluded
      ? data.canInclude
      : data.canExclude
    : false;
  const canChangeMembership = state !== "personal";

  const toggle = () => {
    setExcluded.mutate(
      {
        request: {
          setMemoryExcludedRequestBody: { id: chatId, excluded: !excluded },
        },
      },
      {
        onSuccess: (next) => {
          setChatMemoryData(queryClient, [{ id: chatId }], next);
          void invalidateAllListChats(queryClient);
          void invalidateAllOrgMemoryRecent(queryClient);
          void invalidateAllOrgMemoryDigests(queryClient);
          void invalidateAllOrgMemoryOverview(queryClient);
          toast.success(
            next.state === "excluded"
              ? "Kept out of Organizational Memory. Digests that included it are rebuilt tonight."
              : "Returned to Organizational Memory.",
          );
        },
        onError: (error) => {
          toast.error(
            error.message || "Couldn't change memory for this session",
          );
        },
      },
    );
  };

  return (
    <div className="border-b px-4 py-3">
      <div className="flex items-center justify-between gap-2">
        <button
          type="button"
          onClick={() => setExpanded((prev) => !prev)}
          className="text-foreground inline-flex items-center gap-1.5 text-sm font-medium"
          aria-expanded={expanded}
        >
          <Brain className="size-3.5" aria-hidden />
          Organizational Memory
          {expanded ? (
            <ChevronUp className="text-muted-foreground size-3.5" />
          ) : (
            <ChevronDown className="text-muted-foreground size-3.5" />
          )}
        </button>
        <div className="flex items-center gap-2">
          <MemoryStateBadge state={state} />
          {data && canChangeMembership && (
            <Button
              variant="secondary"
              size="sm"
              disabled={!canToggle || setExcluded.isPending}
              onClick={toggle}
              title={
                canToggle
                  ? undefined
                  : "Only the session's owner or someone with chat:write can return it to memory."
              }
            >
              <Button.Text>
                {setExcluded.isPending
                  ? "Saving…"
                  : excluded
                    ? "Return to memory"
                    : "Exclude"}
              </Button.Text>
            </Button>
          )}
        </div>
      </div>

      {expanded && data && (
        <div className="mt-3 flex flex-col gap-3 text-sm">
          {data.summary ? (
            <div className="flex flex-col gap-1">
              <span className="text-muted-foreground font-mono text-[11px] tracking-wider uppercase">
                What agents read
              </span>
              <p className="text-foreground/90 leading-relaxed whitespace-pre-wrap">
                {data.summary}
              </p>
              {data.summarizedAt && (
                <span className="text-muted-foreground text-xs">
                  Masked summary written{" "}
                  {formatDistanceToNow(data.summarizedAt, { addSuffix: true })}
                </span>
              )}
            </div>
          ) : (
            <p className="text-muted-foreground">
              {state === "personal"
                ? "This session came from a personal or unclassified AI account, so it never enters Organizational Memory."
                : excluded
                  ? "Kept out of Organizational Memory, so agents never read this session."
                  : "No masked summary yet. Memory reads sessions about 30 minutes after they go quiet."}
            </p>
          )}

          {data.memos.length > 0 && (
            <div className="flex flex-col gap-1">
              <span className="text-muted-foreground font-mono text-[11px] tracking-wider uppercase">
                Memos saved from this session
              </span>
              <ul className="flex flex-col gap-1.5">
                {data.memos.map((memo) => (
                  <li key={memo.id} className="leading-relaxed">
                    {memo.content}
                    <span className="text-muted-foreground ml-2 text-xs">
                      {format(memo.createdAt, "MMM d")}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          )}

          {data.dayDigestId && data.dayDigestPeriodStart && !excluded && (
            <Link
              to={`${routes.agentSessions.memory.href()}?level=day&digest=${data.dayDigestId}`}
              className="text-foreground self-start text-xs hover:underline"
            >
              In the day digest for{" "}
              {formatPeriod(
                "day",
                data.dayDigestPeriodStart,
                data.dayDigestPeriodStart,
              )}
            </Link>
          )}
        </div>
      )}
    </div>
  );
}
