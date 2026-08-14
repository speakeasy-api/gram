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
import { invalidateShadowMCPPolicyInventory } from "@/components/shadow-mcp/useShadowMCPPolicyInventory";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import { useCreateMcpApprovalRequestMutation } from "@gram/client/react-query/createMcpApprovalRequest.js";
import { invalidateGetMcpApprovalRequest } from "@gram/client/react-query/getMcpApprovalRequest.js";
import { invalidateAllListMcpApprovalRequests } from "@gram/client/react-query/listMcpApprovalRequests.js";
import { usePromoteMcpApprovalRequestMutation } from "@gram/client/react-query/promoteMcpApprovalRequest.js";
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
 * requested ones. A pending legacy bypass request is promoted into the review
 * first, so the original ask (requester and justification) attaches to it and
 * resolves with the decision instead of staying pending forever.
 */
export type DecideAccessTarget = {
  canonicalServerUrl: string;
  displayName: string;
  approvalRequestId?: string;
  pendingBypassRequestId?: string;
};

export type AccessDecision = "approved" | "denied";

type AudienceGroup = {
  heading: string;
  options: { label: string; value: string }[];
};

/**
 * The grouped role/member options an approval audience is picked from. Values
 * are principal URNs — exactly what recordDecision's granted_principal_urns
 * accepts.
 */
function audienceGroups(
  members: AccessMember[],
  roles: Role[],
): AudienceGroup[] {
  return [
    {
      heading: "Roles",
      options: roles.map((role) => ({
        label: role.name,
        value: role.principalUrn,
      })),
    },
    {
      heading: "Members",
      options: members.map((member) => ({
        label:
          member.name && member.name !== member.email
            ? `${member.name} (${member.email})`
            : member.email,
        value: member.principalUrn,
      })),
    },
  ].filter((group) => group.options.length > 0);
}

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
  const promoteRequest = usePromoteMcpApprovalRequestMutation();
  const decide = useRecordMcpApprovalDecisionMutation();
  const [decision, setDecision] = useState<AccessDecision>("approved");
  const [audience, setAudience] = useState<string[]>([]);
  const [rationale, setRationale] = useState(RATIONALE_PREFILL.approved);
  const [rationaleEdited, setRationaleEdited] = useState(false);
  // The request a previous submit already opened or promoted, so a retry
  // after a failed decision lands on the same review instead of opening
  // another.
  const [openedRequestId, setOpenedRequestId] = useState<string | undefined>(
    undefined,
  );
  // Guards the whole submit, including the invalidation window after the
  // decision lands: the mutations' isPending flags all read false there, and
  // a double click in that window would append a duplicate decision.
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) {
      setDecision("approved");
      setAudience([]);
      setRationale(RATIONALE_PREFILL.approved);
      setRationaleEdited(false);
      setOpenedRequestId(undefined);
      setSubmitting(false);
    }
  }, [open]);

  if (!target) return null;

  // Under an allow-by-default policy a narrow approval is inexpressible —
  // approving clears the block for everyone — so the audience picker only
  // appears when a block-by-default policy can scope who passes.
  const audienceSelectable = disposition !== "allow_all";
  const isSubmitting =
    submitting ||
    createRequest.isPending ||
    promoteRequest.isPending ||
    decide.isPending;
  const rationaleMissing = rationale.trim().length === 0;

  const selectDecision = (next: AccessDecision) => {
    setDecision(next);
    if (!rationaleEdited) {
      setRationale(RATIONALE_PREFILL[next]);
    }
  };

  const submit = async () => {
    const trimmedRationale = rationale.trim();
    let requestId = target.approvalRequestId ?? openedRequestId;
    setSubmitting(true);
    try {
      if (target.pendingBypassRequestId && !openedRequestId) {
        // A pending legacy ask exists: promote it so its requester and
        // justification attach to the review and the ask itself resolves
        // with this decision.
        const summary = await promoteRequest.mutateAsync({
          request: {
            gramProject: project.slug,
            promoteRequestBody: {
              riskPolicyBypassRequestId: target.pendingBypassRequestId,
            },
          },
        });
        requestId = summary.id;
      } else if (!requestId) {
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
    } catch {
      toast.error("Opening the access request failed — nothing was changed");
      setSubmitting(false);
      return;
    }
    setOpenedRequestId(requestId);
    try {
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
      // The request row exists (and now sits in the queue); only the
      // decision failed. Saying "nothing changed" here would be a lie — and
      // an existing review that just absorbed a promoted bypass request is
      // not unchanged either.
      toast.error(
        target.approvalRequestId && !target.pendingBypassRequestId
          ? "Recording the decision failed — the request is unchanged"
          : "The access request was opened or updated, but recording the decision failed — retry to decide it",
      );
      if (!target.approvalRequestId || target.pendingBypassRequestId) {
        // This submit changed request state before failing; refresh the
        // affected views so the pending request is visible.
        await Promise.all([
          invalidateAllShadowMCPInventory(queryClient),
          invalidateAllShadowMCPInventoryServer(queryClient),
          invalidateAllListMcpApprovalRequests(queryClient),
        ]);
      }
      setSubmitting(false);
      return;
    }
    await Promise.all([
      invalidateAllShadowMCPInventory(queryClient),
      invalidateAllShadowMCPInventoryServer(queryClient),
      invalidateAllListMcpApprovalRequests(queryClient),
      invalidateGetMcpApprovalRequest(queryClient, [{ id: requestId }]),
      // The policy editor seeds its URL sets from a grant-derived cache; a
      // decision just rewrote those grants, and a stale cache would let the
      // next policy save silently revert this decision's enforcement.
      invalidateShadowMCPPolicyInventory(queryClient, project.id),
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
                  decision === "approved" && "border-border bg-card",
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
                  decision === "denied" && "border-border bg-card",
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
