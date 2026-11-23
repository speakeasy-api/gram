import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Icon } from "@/components/ui/Icon";
import { Spinner } from "@/components/ui/Spinner";
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
import {
  applicableDecision,
  decisionActionLabel,
} from "@/components/shadow-mcp/shadowMCPPolicyChange";
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
import { audienceGroups } from "./audienceGroups";
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
  /**
   * The reference namespace being decided on. Defaults to server_url; a
   * stdio_command target carries the redacted command in canonicalServerUrl,
   * has no URL for enforcement grants, and records a decision of record
   * instead.
   */
  targetKind?: "server_url" | "stdio_command";
  canonicalServerUrl: string;
  displayName: string;
  approvalRequestId?: string;
  pendingBypassRequestId?: string;
  /** The decision already recorded on the server, if any. */
  currentDecision?: AccessDecision;
};

export type AccessDecision = "approved" | "denied";

/**
 * Why deciding waits while the policy list has failed to load: the sheet
 * writes grants against the policies it can see, so with none loaded an
 * approval would record a decision that enforces nothing. Every surface that
 * opens the sheet says this the same way.
 */
export const POLICIES_UNAVAILABLE_REASON =
  "Policy data could not be loaded — deciding now would not be enforced";

const RATIONALE_PREFILL: Record<AccessDecision, string> = {
  approved: "Approved for use in this project.",
  denied: "Blocked for use in this project.",
};

function approveSublabel(stdio: boolean, narrowed: boolean): string {
  if (stdio) return "Record the approval as the decision of record.";
  if (narrowed) return "Allow the server only for the people or roles below.";
  return "Unblock the server for everyone in the project.";
}

function denySublabel(stdio: boolean, narrowed: boolean): string {
  if (stdio) return "Record the denial as the decision of record.";
  if (narrowed) return "Block the server only for the people or roles below.";
  return "Block the server for everyone in the project.";
}

/**
 * Who a decision covers, as sent to the API: nothing means everyone. An
 * approval uses the approval audience and a denial its own.
 */
function grantedPrincipalUrns(
  decision: AccessDecision,
  approveAudience: string[],
  denyAudience: string[],
): string[] | undefined {
  const audience = decision === "approved" ? approveAudience : denyAudience;
  return audience.length > 0 ? audience : undefined;
}

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
  const initialDecision = applicableDecision(
    disposition,
    target?.currentDecision,
  );
  const [decision, setDecision] = useState<AccessDecision>(initialDecision);
  const [audience, setAudience] = useState<string[]>([]);
  const [denyAudience, setDenyAudience] = useState<string[]>([]);
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
    if (!open) return;
    // Every opening starts on the action the policy makes applicable to
    // this server, with a clean form.
    setDecision(initialDecision);
    setAudience([]);
    setDenyAudience([]);
    setRationale(RATIONALE_PREFILL[initialDecision]);
    setRationaleEdited(false);
    setOpenedRequestId(undefined);
    setSubmitting(false);
    // The applicable action also moves when the policy list loads after the
    // sheet opened, so the form follows it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, target?.canonicalServerUrl, initialDecision]);

  if (!target) return null;

  const stdio = target.targetKind === "stdio_command";
  // A narrowed approval lets only its people or roles through. A Shadow AI
  // policy that allows servers by default can only clear a block for
  // everyone, so it offers no audience. A stdio command has no URL for grants
  // to attach to, so it never shows one.
  const audienceSelectable = disposition !== "allow_all" && !stdio;
  // A denial can be narrowed to people or roles wherever servers are allowed
  // by default: it blocks only them. Under blocked by default a denial
  // withdraws the approval, which already blocks everyone.
  const denyAudienceSelectable = disposition !== "block_all" && !stdio;
  const pickerShown =
    decision === "approved" ? audienceSelectable : denyAudienceSelectable;
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
              targetKind: target.targetKind ?? "server_url",
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
            grantedPrincipalUrns: grantedPrincipalUrns(
              decision,
              audienceSelectable ? audience : [],
              denyAudienceSelectable ? denyAudience : [],
            ),
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
        : `Blocked: ${target.displayName}`,
    );
    onDecided?.(decision);
    onOpenChange(false);
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>{decisionActionLabel(decision)}</SheetTitle>
          <SheetDescription>
            The decision is recorded with its rationale and enforced in this
            project, whether servers are allowed or blocked by default.
          </SheetDescription>
        </SheetHeader>

        <RequireScope scope="org:admin" level="component">
          <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-4">
            <section className="border-border border px-4 py-3">
              <Text variant="small" className="font-medium break-all">
                {target.displayName}
              </Text>
              {target.canonicalServerUrl !== target.displayName && (
                <Text muted small className="mt-1 break-all">
                  {target.canonicalServerUrl}
                </Text>
              )}
            </section>

            <Text small>
              {decision === "approved"
                ? approveSublabel(
                    stdio,
                    audienceSelectable && audience.length > 0,
                  )
                : denySublabel(
                    stdio,
                    denyAudienceSelectable && denyAudience.length > 0,
                  )}
            </Text>

            <Collapsible>
              <CollapsibleTrigger className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-sm transition-colors [&[data-state=open]>svg]:rotate-90">
                <Icon name="chevron-right" className="size-3.5" aria-hidden />
                Advanced
              </CollapsibleTrigger>
              <CollapsibleContent className="mt-3 space-y-4">
                <RadioGroup
                  value={decision}
                  onValueChange={(value) =>
                    selectDecision(value as AccessDecision)
                  }
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
                        {approveSublabel(
                          stdio,
                          audienceSelectable && audience.length > 0,
                        )}
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
                        <Badge.Text>Block</Badge.Text>
                      </Badge>
                      <Text muted small>
                        {denySublabel(
                          stdio,
                          denyAudienceSelectable && denyAudience.length > 0,
                        )}
                      </Text>
                    </span>
                  </label>
                </RadioGroup>

                {pickerShown && (
                  // Keyed by decision so each decision keeps its own
                  // audience while the admin switches between them.
                  <section
                    key={decision}
                    className="border-border space-y-2 border p-3"
                  >
                    <Text variant="small" className="font-medium">
                      {decision === "approved"
                        ? "Who the approval covers"
                        : "Who the block covers"}
                    </Text>
                    <MultiSelect
                      options={audienceGroups(members, roles)}
                      defaultValue={
                        decision === "approved" ? audience : denyAudience
                      }
                      onValueChange={
                        decision === "approved" ? setAudience : setDenyAudience
                      }
                      placeholder="Everyone in the project"
                    />
                    <Text muted small>
                      {decision === "approved"
                        ? "Pick people or roles to allow the server only for them; everyone else is blocked. Leave empty to approve for everyone."
                        : "Pick people or roles to block the server only for them. Leave empty to block for everyone."}
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
              </CollapsibleContent>
            </Collapsible>

            {stdio && (
              <Text muted small>
                Shared with the requester. This is the decision of record — a
                command-line server has no URL for blocking policies to enforce
                against automatically.
              </Text>
            )}
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
                {isSubmitting && <Spinner className="mr-0" />}
              </Button.LeftIcon>
              <Button.Text>
                {decision === "approved" ? "Approve Server" : "Block Server"}
              </Button.Text>
            </Button>
          </SheetFooter>
        </RequireScope>
      </SheetContent>
    </Sheet>
  );
}
