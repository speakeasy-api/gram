import { Button } from "@/components/ui/Button";
import { Separator } from "@/components/ui/Separator";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import type { RiskExclusion } from "@gram/client/models/components/riskexclusion.js";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { format } from "date-fns";
import type { ReactNode } from "react";
import { CategoryLabel } from "../risk-ui";
import { getRuleTitleFallback } from "../risk-utils";
import {
  isRestorable,
  suppressionDetail,
  suppressionReason,
  SUPPRESSION_REASON_LABEL,
} from "./suppressed-helpers";

const TIMESTAMP_FORMAT = "MMM d, yyyy h:mm a";

/**
 * Detail view for one suppressed finding. Deliberately read-only about the
 * finding itself: the active-signal drawer's "create exclusion" and "suppress"
 * actions have nothing to act on here — this finding is already suppressed —
 * so the only actions are the two that reverse or explain the suppression.
 */
export function SuppressedFindingDrawer({
  finding,
  exclusion,
  onClose,
  onRestore,
  onViewRule,
  onViewSession,
}: {
  /** The finding to detail; null keeps the drawer closed. */
  finding: RiskResult | null;
  /** The exclusion behind a rule suppression, when it still exists. */
  exclusion: RiskExclusion | undefined;
  onClose: () => void;
  onRestore: (id: string) => void;
  onViewRule: () => void;
  onViewSession: (chatId: string) => void;
}): JSX.Element {
  return (
    <Sheet
      open={finding !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent side="right" className="w-full overflow-y-auto sm:max-w-xl">
        {finding && (
          <>
            <SheetHeader>
              <SheetTitle>{getRuleTitleFallback(finding.ruleId)}</SheetTitle>
              <SheetDescription>
                Suppressed — this finding stays out of the risk score and every
                finding count until it is restored.
              </SheetDescription>
            </SheetHeader>
            <div className="space-y-6 px-4 pb-6">
              <SuppressionSection finding={finding} exclusion={exclusion} />
              <Separator />
              <FindingSection finding={finding} />
              <Separator />
              <EvidenceSection finding={finding} />
              <SessionSection finding={finding} onViewSession={onViewSession} />
            </div>
            <SheetFooter>
              <FindingActions
                finding={finding}
                onRestore={onRestore}
                onViewRule={onViewRule}
              />
            </SheetFooter>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}

function DetailRow({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-eyebrow">{label}</span>
      <div className="min-w-0 text-sm">{children}</div>
    </div>
  );
}

function SuppressionSection({
  finding,
  exclusion,
}: {
  finding: RiskResult;
  exclusion: RiskExclusion | undefined;
}): JSX.Element {
  const reason = suppressionReason(finding);
  const detail = suppressionDetail(finding, exclusion);
  return (
    <div className="space-y-4">
      <DetailRow label="Reason">{SUPPRESSION_REASON_LABEL[reason]}</DetailRow>
      {detail && (
        <DetailRow label={reason === "rule" ? "Exclusion rule" : "Note"}>
          <span className="font-mono break-all">{detail}</span>
        </DetailRow>
      )}
      {finding.suppressedAt && (
        <DetailRow label="Suppressed">
          {format(finding.suppressedAt, TIMESTAMP_FORMAT)}
        </DetailRow>
      )}
    </div>
  );
}

function FindingSection({ finding }: { finding: RiskResult }): JSX.Element {
  return (
    <div className="space-y-4">
      <DetailRow label="Category">
        <CategoryLabel source={finding.source} ruleId={finding.ruleId} />
      </DetailRow>
      {finding.ruleId && (
        <DetailRow label="Rule">
          <span className="font-mono break-all">{finding.ruleId}</span>
        </DetailRow>
      )}
      {finding.description && (
        <DetailRow label="Description">{finding.description}</DetailRow>
      )}
      {finding.confidence != null && (
        <DetailRow label="Confidence">
          {finding.confidence.toFixed(2)}
        </DetailRow>
      )}
      <DetailRow label="Detected">
        {format(finding.createdAt, TIMESTAMP_FORMAT)}
      </DetailRow>
    </div>
  );
}

/**
 * The redacted match fingerprint, with no reveal affordance. The unmask
 * endpoint refuses suppressed findings outright (its query filters
 * `excluded_at IS NULL` and `false_positive_at IS NULL`), so the
 * click-to-reveal control the other finding surfaces use would be a button
 * that can never succeed here.
 */
function EvidenceSection({ finding }: { finding: RiskResult }): JSX.Element {
  if (!finding.matchRedacted) return <></>;
  return (
    <div className="space-y-4">
      <DetailRow label="Match">
        <span className="border-border inline-block max-w-full font-mono text-xs break-all">
          {finding.matchRedacted}
        </span>
      </DetailRow>
      <Text small muted>
        The matched value can't be revealed while the finding is suppressed.
        Restore it first.
      </Text>
    </div>
  );
}

function SessionSection({
  finding,
  onViewSession,
}: {
  finding: RiskResult;
  onViewSession: (chatId: string) => void;
}): JSX.Element {
  const chatId = finding.chatId;
  if (!chatId) return <></>;
  return (
    <>
      <Separator />
      <div className="space-y-4">
        <DetailRow label="Session">
          <span className="truncate">{finding.chatTitle ?? "Untitled"}</span>
        </DetailRow>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => onViewSession(chatId)}
        >
          <Button.Text>View session</Button.Text>
        </Button>
      </div>
    </>
  );
}

function FindingActions({
  finding,
  onRestore,
  onViewRule,
}: {
  finding: RiskResult;
  onRestore: (id: string) => void;
  onViewRule: () => void;
}): JSX.Element {
  if (!isRestorable(finding)) {
    return (
      <Button variant="secondary" onClick={onViewRule}>
        <Button.Text>View rule</Button.Text>
      </Button>
    );
  }
  return (
    <Button variant="primary" onClick={() => onRestore(finding.id)}>
      <Button.Text>Restore</Button.Text>
    </Button>
  );
}
