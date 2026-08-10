import { audienceGroups } from "@/components/mcp-approvals/audience";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { MultiSelect } from "@/components/ui/MultiSelect";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
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
import { useProject } from "@/contexts/Auth";
import { cn } from "@/lib/utils";
import type { ShadowMCPPolicyDisposition } from "@/components/shadow-mcp/shadowMCPInventoryStatus";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import { useCreateMcpApprovalRequestMutation } from "@gram/client/react-query/createMcpApprovalRequest.js";
import { invalidateGetMcpApprovalRequest } from "@gram/client/react-query/getMcpApprovalRequest.js";
import { invalidateAllListMcpApprovalRequests } from "@gram/client/react-query/listMcpApprovalRequests.js";
import { useRecordMcpApprovalDecisionMutation } from "@gram/client/react-query/recordMcpApprovalDecision.js";
import { invalidateAllShadowMCPInventory } from "@gram/client/react-query/shadowMCPInventory.js";
import { invalidateAllShadowMCPInventoryServer } from "@gram/client/react-query/shadowMCPInventoryServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";

/**
 * The server being decided on. A decision needs the canonical URL (the
 * identity everything keys on), a name to address it by, and — when a review
 * already exists — the request the decision attaches to. Without a request id
 * the sheet opens one first, so proactive decisions travel the same road as
 * requested ones.
 */
export type DecideAccessTarget = {
  canonicalServerUrl: string;
  displayName: string;
  approvalRequestId?: string;
};

export type AccessDecision = "approved" | "denied";

const RATIONALE_PREFILL: Record<AccessDecision, string> = {
  approved: "Approved for use in this project.",
  denied: "Denied for use in this project.",
};

/**
 * Allow or deny one MCP server. This is the single write path for server
 * access: the decision is recorded on the server's approval request (opened
 * on the spot when none exists) and the recorded decision is what mints or
 * revokes the enforcement grants — there is no separate rule to manage.
 */
export function DecideAccessSheet({
  target,
  open,
  onOpenChange,
  disposition,
  members,
  roles,
  onDecided,
}: {
  target: DecideAccessTarget | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  disposition: ShadowMCPPolicyDisposition | null;
  members: AccessMember[];
  roles: Role[];
  onDecided?: (decision: AccessDecision) => void;
}): JSX.Element | null {
  const project = useProject();
  const queryClient = useQueryClient();
  const createRequest = useCreateMcpApprovalRequestMutation();
  const decide = useRecordMcpApprovalDecisionMutation();
  const [decision, setDecision] = useState<AccessDecision>("approved");
  const [audience, setAudience] = useState<string[]>([]);
  const [rationale, setRationale] = useState(RATIONALE_PREFILL.approved);
  const [rationaleEdited, setRationaleEdited] = useState(false);

  useEffect(() => {
    if (!open) {
      setDecision("approved");
      setAudience([]);
      setRationale(RATIONALE_PREFILL.approved);
      setRationaleEdited(false);
    }
  }, [open]);

  if (!target) return null;

  // Under an allow-by-default policy a narrow approval is inexpressible —
  // approving clears the block for everyone — so the audience picker only
  // appears when a block-by-default policy can scope who passes.
  const audienceSelectable = disposition !== "allow_all";
  const isSubmitting = createRequest.isPending || decide.isPending;
  const rationaleMissing = rationale.trim().length === 0;

  const selectDecision = (next: AccessDecision) => {
    setDecision(next);
    if (!rationaleEdited) {
      setRationale(RATIONALE_PREFILL[next]);
    }
  };

  const submit = async () => {
    const trimmedRationale = rationale.trim();
    let requestId = target.approvalRequestId;
    try {
      if (!requestId) {
        const summary = await createRequest.mutateAsync({
          request: {
            gramProject: project.slug,
            createRequestRequestBody: {
              targetKind: "server_url",
              target: target.canonicalServerUrl,
              note: trimmedRationale,
            },
          },
        });
        requestId = summary.id;
      }
      await decide.mutateAsync({
        request: {
          gramProject: project.slug,
          recordDecisionRequestBody: {
            id: requestId,
            decision,
            rationale: trimmedRationale,
            grantedPrincipalUrns:
              decision === "approved" && audience.length > 0
                ? audience
                : undefined,
          },
        },
      });
    } catch {
      toast.error("Recording the decision failed — nothing was changed");
      return;
    }
    await Promise.all([
      invalidateAllShadowMCPInventory(queryClient),
      invalidateAllShadowMCPInventoryServer(queryClient),
      invalidateAllListMcpApprovalRequests(queryClient),
      invalidateGetMcpApprovalRequest(queryClient, [{ id: requestId }]),
    ]);
    toast.success(
      decision === "approved"
        ? `Approved: ${target.displayName}`
        : `Denied: ${target.displayName}`,
    );
    onDecided?.(decision);
    onOpenChange(false);
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>Decide access</SheetTitle>
          <SheetDescription>
            The decision is recorded with its rationale and enforced across
            every blocking policy in this project.
          </SheetDescription>
        </SheetHeader>

        <RequireScope scope="mcp_approval:decide" level="component">
          <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-4">
            <section className="border-border border px-4 py-3">
              <Text variant="small" className="font-medium">
                {target.displayName}
              </Text>
              <Text muted small className="mt-1 break-all">
                {target.canonicalServerUrl}
              </Text>
            </section>

            <RadioGroup
              value={decision}
              onValueChange={(value) => selectDecision(value as AccessDecision)}
              className="border-border grid grid-cols-2 gap-4 border p-3"
            >
              <label
                className={cn(
                  "flex cursor-pointer items-start gap-3 border border-transparent px-3 py-2.5 transition-colors",
                  decision === "approved" && "border-border bg-card shadow-xs",
                )}
              >
                <RadioGroupItem value="approved" className="mt-1.5" />
                <span>
                  <Badge variant="success">
                    <Badge.Text>Approve</Badge.Text>
                  </Badge>
                  <Text muted small>
                    {audienceSelectable
                      ? "Allow the server for the audience below."
                      : "Unblock the server for everyone in the project."}
                  </Text>
                </span>
              </label>
              <label
                className={cn(
                  "flex cursor-pointer items-start gap-3 border border-transparent px-3 py-2.5 transition-colors",
                  decision === "denied" && "border-border bg-card shadow-xs",
                )}
              >
                <RadioGroupItem value="denied" className="mt-1.5" />
                <span>
                  <Badge variant="destructive">
                    <Badge.Text>Deny</Badge.Text>
                  </Badge>
                  <Text muted small>
                    Block the server for everyone in the project.
                  </Text>
                </span>
              </label>
            </RadioGroup>

            {decision === "approved" && audienceSelectable && (
              <section className="border-border space-y-2 border p-3">
                <Text variant="small" className="font-medium">
                  Who the approval covers
                </Text>
                <MultiSelect
                  options={audienceGroups(members, roles)}
                  defaultValue={audience}
                  onValueChange={setAudience}
                  placeholder="Everyone in the project"
                />
                <Text muted small>
                  Leave empty to approve for everyone.
                </Text>
              </section>
            )}

            <section className="border-border space-y-2 border p-3">
              <Text variant="small" className="font-medium">
                Rationale
              </Text>
              <TextArea
                value={rationale}
                onChange={(value) => {
                  setRationale(value);
                  setRationaleEdited(true);
                }}
                rows={3}
                className="resize-none text-sm"
              />
              <Text muted small>
                Shared with anyone who asks for this server.
              </Text>
            </section>
          </div>

          <SheetFooter>
            <Button
              className="w-full"
              disabled={isSubmitting || rationaleMissing}
              onClick={() => void submit()}
              variant={
                decision === "denied" ? "destructive-primary" : "primary"
              }
            >
              <Button.LeftIcon>
                {isSubmitting && (
                  <Icon name="loader-circle" className="animate-spin" />
                )}
              </Button.LeftIcon>
              <Button.Text>
                {decision === "approved" ? "Approve Server" : "Deny Server"}
              </Button.Text>
            </Button>
          </SheetFooter>
        </RequireScope>
      </SheetContent>
    </Sheet>
  );
}
