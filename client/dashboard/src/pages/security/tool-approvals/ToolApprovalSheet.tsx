import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import { TextArea } from "@/components/ui/Textarea";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { RiskPolicyApproval } from "@gram/client/models/components/riskpolicyapproval.js";
import { useRiskDecideApprovalMutation } from "@gram/client/react-query/riskDecideApproval.js";
import { invalidateAllRiskGetApproval } from "@gram/client/react-query/riskGetApproval.js";
import { invalidateAllRiskListApprovals } from "@gram/client/react-query/riskListApprovals.js";
import { useQueryClient } from "@tanstack/react-query";
import { format } from "date-fns";
import { useState } from "react";
import { toast } from "sonner";
import { APPROVAL_STATUS_BADGE, requesterLabel } from "./approval-labels";

type Decision = "approve" | "deny";

function DetailRow({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="grid grid-cols-[120px_minmax(0,1fr)] gap-3 py-1.5">
      <Text small muted>
        {label}
      </Text>
      <div className="min-w-0 text-sm">{children}</div>
    </div>
  );
}

/**
 * One tool approval request: what was called, with what, and why, and, for
 * an approver while it is pending, Approve once and Deny. The same decision
 * as the Slack card; only listed approvers see the buttons.
 */
export function ToolApprovalSheet({
  approval,
  members,
  onOpenChange,
}: {
  approval: RiskPolicyApproval | null;
  members: AccessMember[];
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [note, setNote] = useState("");
  const [pending, setPending] = useState<Decision | null>(null);
  const mutation = useRiskDecideApprovalMutation({
    onSuccess: (decided) => {
      void invalidateAllRiskListApprovals(queryClient);
      void invalidateAllRiskGetApproval(queryClient);
      toast.success(
        decided.status === "approved"
          ? `Approved one call to ${decided.toolName}`
          : `Denied the call to ${decided.toolName}`,
      );
      setNote("");
      onOpenChange(false);
    },
    onError: (error) => {
      toast.error(error.message || "The decision could not be saved.");
    },
    onSettled: () => setPending(null),
  });

  const decide = (decision: Decision) => {
    if (!approval) return;
    setPending(decision);
    mutation.mutate({
      request: {
        riskPolicyApprovalDecisionRequestBody: {
          id: approval.id,
          decision,
          ...(note.trim() ? { note: note.trim() } : {}),
        },
      },
    });
  };

  const status = approval ? APPROVAL_STATUS_BADGE[approval.status] : null;
  return (
    <Sheet open={approval !== null} onOpenChange={onOpenChange}>
      <SheetContent className="flex flex-col sm:max-w-xl">
        {approval && status ? (
          <>
            <SheetHeader>
              <SheetTitle className="font-mono">{approval.toolName}</SheetTitle>
              <SheetDescription>
                Held by {approval.policyName} for{" "}
                {requesterLabel(approval.requesterUrn, members)}
              </SheetDescription>
            </SheetHeader>
            <div className="flex-1 space-y-4 overflow-y-auto px-4">
              <section className="border-border border p-3">
                <DetailRow label="Status">
                  <Badge variant={status.variant}>
                    <Badge.Text>{status.label}</Badge.Text>
                  </Badge>
                </DetailRow>
                <DetailRow label="Reason">
                  {approval.reason ?? (
                    <Text small muted>
                      The agent has not requested approval yet.
                    </Text>
                  )}
                </DetailRow>
                <DetailRow label="Covers">
                  One retry of this exact call
                </DetailRow>
                <DetailRow label="Held">
                  {format(approval.createdAt, "PPp")}
                </DetailRow>
                {approval.expiresAt ? (
                  <DetailRow label="Expires">
                    {format(approval.expiresAt, "PPp")}
                  </DetailRow>
                ) : null}
                {approval.decidedByUrn ? (
                  <DetailRow label="Decided by">
                    {requesterLabel(approval.decidedByUrn, members)}
                    {approval.decidedVia ? ` via ${approval.decidedVia}` : ""}
                  </DetailRow>
                ) : null}
                {approval.decisionNote ? (
                  <DetailRow label="Note">{approval.decisionNote}</DetailRow>
                ) : null}
              </section>
              <section className="space-y-2">
                <Text variant="small" className="font-medium">
                  Arguments
                </Text>
                <pre className="bg-muted/40 border-border max-h-80 overflow-auto border p-3 font-mono text-xs whitespace-pre-wrap">
                  {JSON.stringify(approval.argumentsPreview, null, 2)}
                </pre>
                <Text small muted>
                  Long values are shortened. The retry must send exactly these
                  arguments.
                </Text>
              </section>
              {approval.canDecide ? (
                <section className="space-y-2">
                  <Text variant="small" className="font-medium">
                    Note for the agent
                  </Text>
                  <TextArea
                    value={note}
                    onChange={setNote}
                    rows={3}
                    placeholder="Optional"
                    className="resize-none text-sm"
                  />
                </section>
              ) : null}
            </div>
            {approval.canDecide ? (
              <SheetFooter className="grid grid-cols-2 gap-3">
                <Button
                  variant="destructive-primary"
                  disabled={mutation.isPending}
                  onClick={() => decide("deny")}
                >
                  <Button.LeftIcon>
                    {pending === "deny" && (
                      <Icon name="loader-circle" className="animate-spin" />
                    )}
                  </Button.LeftIcon>
                  <Button.Text>Deny</Button.Text>
                </Button>
                <Button
                  variant="primary"
                  disabled={mutation.isPending}
                  onClick={() => decide("approve")}
                >
                  <Button.LeftIcon>
                    {pending === "approve" && (
                      <Icon name="loader-circle" className="animate-spin" />
                    )}
                  </Button.LeftIcon>
                  <Button.Text>Approve once</Button.Text>
                </Button>
              </SheetFooter>
            ) : null}
          </>
        ) : null}
      </SheetContent>
    </Sheet>
  );
}
