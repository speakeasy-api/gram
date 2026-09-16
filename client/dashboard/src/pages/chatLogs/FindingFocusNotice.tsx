import { ArrowDownToLine, Loader2, Lock, ShieldAlert } from "lucide-react";
import type { ReactNode } from "react";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { Badge } from "@/components/ui/Badge";
import { REVEAL_DENIED_REASON } from "@/pages/security/unmask";
import {
  getMatchStrings,
  getRiskBadgeLabel,
  hasHiddenMatch,
  shouldShowRiskRuleId,
} from "./chatHelpers";

/** Where the flagged message ended up relative to the loaded transcript. */
type Location =
  | "highlighted" // on screen and ringed, with its matched span marked
  | "wholeMessage" // on screen and ringed, but the finding marks no span in it
  | "masked" // on screen and ringed, but the matched value was withheld
  | "unloaded"; // not in the loaded window, so there is nothing to ring yet

function locationOf(finding: RiskResult, located: boolean): Location {
  if (!located) return "unloaded";
  if (hasHiddenMatch([finding])) return "masked";
  // A judge finding's "match" is the whole event it read, and account_identity
  // matches session metadata — neither marks a span, so promising a
  // highlighted value would send the reader hunting for one.
  if (getMatchStrings([finding]).length === 0) return "wholeMessage";
  return "highlighted";
}

function sentenceFor(location: Location): string {
  switch (location) {
    case "highlighted":
      return "The matched value is highlighted in the message below.";
    case "wholeMessage":
      return "The message it flagged is highlighted below.";
    case "masked":
      return `The message it was found in is highlighted below, but the value itself stays masked. ${REVEAL_DENIED_REASON}`;
    case "unloaded":
      return "Its message isn't part of the loaded transcript — load all messages to see it.";
  }
}

function NoticeBar({ children }: { children: ReactNode }) {
  return (
    <div className="flex shrink-0 flex-wrap items-center gap-x-2 gap-y-1 border-b px-4 py-2 text-xs">
      {children}
    </div>
  );
}

/**
 * Strip above the transcript naming the finding the panel was opened from.
 *
 * Selecting an evidence row in Risk Events / Risk Overview drops the reader
 * mid-session, where a red turn divider is the only hint of what they came to
 * look at — and when the matched value was withheld (no chat:read) even the
 * message carries no highlight. This says which finding is in focus, where its
 * message is, and why the value may not be readable.
 */
export function FindingFocusNotice({
  finding,
  isLoading,
  located,
  onJump,
}: {
  /** The focused finding, or undefined once loading finished without it. */
  finding: RiskResult | undefined;
  isLoading: boolean;
  /** Its message is in the loaded transcript, so jumping to it works. */
  located: boolean;
  onJump: () => void;
}): JSX.Element {
  if (isLoading) {
    return (
      <NoticeBar>
        <Loader2 className="text-muted-foreground size-3.5 shrink-0 animate-spin" />
        <span className="text-muted-foreground">
          Locating the flagged message…
        </span>
      </NoticeBar>
    );
  }

  if (!finding) {
    return (
      <NoticeBar>
        <ShieldAlert className="text-muted-foreground size-3.5 shrink-0" />
        <span className="text-muted-foreground">
          This finding is no longer open for this session — it may have been
          suppressed since the list was loaded.
        </span>
      </NoticeBar>
    );
  }

  const location = locationOf(finding, located);
  return (
    <NoticeBar>
      <ShieldAlert className="text-destructive size-3.5 shrink-0" />
      <span className="font-medium">Opened from evidence</span>
      <Badge variant="destructive" className="shrink-0 text-[10px]">
        <Badge.Text>{getRiskBadgeLabel(finding)}</Badge.Text>
      </Badge>
      {shouldShowRiskRuleId(finding) && (
        <span className="text-muted-foreground min-w-0 truncate font-mono">
          {finding.ruleId}
        </span>
      )}
      {location === "masked" && (
        <Lock
          role="img"
          aria-label={REVEAL_DENIED_REASON}
          className="text-muted-foreground size-3 shrink-0"
        />
      )}
      <span className="text-muted-foreground min-w-0">
        {sentenceFor(location)}
      </span>
      {located && (
        <button
          type="button"
          onClick={onJump}
          className="text-muted-foreground hover:text-foreground ml-auto inline-flex shrink-0 cursor-pointer items-center gap-1 font-medium transition-colors"
        >
          <ArrowDownToLine className="size-3" />
          Jump to message
        </button>
      )}
    </NoticeBar>
  );
}
