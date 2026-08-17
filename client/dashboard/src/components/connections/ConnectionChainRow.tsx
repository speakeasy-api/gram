import { ArrowRight } from "lucide-react";

import { ClientSourceBadge } from "@/components/sessions/ClientSourceBadge";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { Text } from "@/components/ui/Text";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import {
  CONNECTION_STATE_PRESENTATION,
  connectionActivityLabel,
  connectionDeadlineLabel,
  connectionState,
} from "@/lib/connection-state";
import { getInitials } from "@/lib/initials";
import { subjectLabel } from "@/lib/user-session-status";
import { cn } from "@/lib/utils";

import type { UserSession } from "@gram/client/models/components/usersession.js";
import type { UserSessionUpstream } from "@gram/client/models/components/usersessionupstream.js";

/**
 * How many upstream providers to name before collapsing the rest into a count.
 * One reads as a sentence; past that the row stops being scannable, and the
 * exact set matters less than the fact that there is more than one.
 */
const NAMED_UPSTREAM_LIMIT = 1;

function ConnectionStateDot({ className }: { className: string }): JSX.Element {
  // bg-current inherits the tone's text color, so the dot needs no palette of
  // its own and can never drift from the label beside it.
  return (
    <span
      className={cn("size-1.5 shrink-0 rounded-full bg-current", className)}
      aria-hidden
    />
  );
}

function UpstreamSummary({
  upstreams,
}: {
  upstreams: UserSessionUpstream[];
}): JSX.Element {
  if (upstreams.length === 0) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="text-muted-foreground">Gram tools only</span>
        </TooltipTrigger>
        <TooltipContent>
          This connection reaches Gram-native tools. Gram holds no upstream
          credentials on this subject&apos;s behalf.
        </TooltipContent>
      </Tooltip>
    );
  }

  const named = upstreams.slice(0, NAMED_UPSTREAM_LIMIT);
  const remaining = upstreams.length - named.length;

  return (
    <span className="inline-flex items-center gap-1">
      {named.map((upstream) => (
        <span key={upstream.remoteSessionId}>{upstream.issuerSlug}</span>
      ))}
      {remaining > 0 ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="text-muted-foreground">+{remaining}</span>
          </TooltipTrigger>
          <TooltipContent>
            {upstreams
              .slice(NAMED_UPSTREAM_LIMIT)
              .map((upstream) => upstream.issuerSlug)
              .join(", ")}
          </TooltipContent>
        </Tooltip>
      ) : null}
    </span>
  );
}

/**
 * One brokered connection, read left to right as a single sentence: the client
 * an agent connects through, the Gram server it reaches, and the upstream Gram
 * reaches on the subject's behalf.
 *
 * The chain is the point. Showing only the inbound half — which is what a
 * sessions table does — leaves an admin unable to tell a working connection
 * from one whose upstream grant has quietly died.
 */
export function ConnectionChainRow({
  session,
  actions,
  showSubject = false,
}: {
  session: UserSession;
  actions?: React.ReactNode;
  /** Shown when rows are not already grouped under a subject heading. */
  showSubject?: boolean;
}): JSX.Element {
  const state = connectionState(session);
  const presentation = CONNECTION_STATE_PRESENTATION[state];
  const upstreams = session.upstreams ?? [];

  return (
    <div className="flex items-start justify-between gap-4 px-4 py-3">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
          <span
            className={cn(
              "inline-flex items-center gap-1.5",
              presentation.toneClass,
            )}
          >
            <ConnectionStateDot className={presentation.toneClass} />
            {presentation.label}
          </span>

          {showSubject ? (
            <span className="inline-flex items-center gap-1.5">
              {/* Only user subjects have a face; API keys and anonymous
                  sessions get the name alone rather than initials of a
                  label that names no person. */}
              {session.subjectType === "user" ? (
                <Avatar className="size-5 shrink-0">
                  {session.subjectPhotoUrl ? (
                    <AvatarImage
                      src={session.subjectPhotoUrl}
                      alt={subjectLabel(session)}
                    />
                  ) : null}
                  <AvatarFallback className="text-[8px] font-semibold">
                    {getInitials(subjectLabel(session))}
                  </AvatarFallback>
                </Avatar>
              ) : null}
              <span className="text-foreground font-medium">
                {subjectLabel(session)}
              </span>
            </span>
          ) : null}

          <span className="text-foreground truncate">
            {session.clientName ?? "Unknown client"}
          </span>
          <ArrowRight className="text-muted-foreground size-3 shrink-0" />
          <span className="text-foreground truncate">{session.issuerSlug}</span>
          <ArrowRight className="text-muted-foreground size-3 shrink-0" />
          <UpstreamSummary upstreams={upstreams} />
        </div>

        <div className="text-muted-foreground mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
          <ClientSourceBadge client={session} />
          <span>{connectionActivityLabel(session.lastUsedAt)}</span>
          <span>{connectionDeadlineLabel(session)}</span>
          {upstreams.some((upstream) => upstream.autoRefresh) ? (
            <Text small muted>
              auto-refresh on
            </Text>
          ) : null}
        </div>
      </div>

      {actions ? <div className="shrink-0">{actions}</div> : null}
    </div>
  );
}
