import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { TextArea } from "@/components/ui/Textarea";
import { invalidateAllListMcpApprovalRequests } from "@gram/client/react-query/listMcpApprovalRequests.js";
import { invalidateGetMcpApprovalRequest } from "@gram/client/react-query/getMcpApprovalRequest.js";
import { useRecordMcpApprovalDecisionMutation } from "@gram/client/react-query/recordMcpApprovalDecision.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

/**
 * Approve or deny, with the rationale the decision will be explained by.
 *
 * The rationale is required by the API — it is the artifact cited when the
 * requester asks why — so the form enforces it before submitting rather than
 * round-tripping for the error. Blast-radius selection (who an approval
 * covers) is a separate ticket; until it lands an approval records the
 * decision without granting principals.
 */
export function DecisionForm({
  requestId,
  projectSlug,
}: {
  requestId: string;
  projectSlug: string;
}): JSX.Element {
  const [rationale, setRationale] = useState("");
  const queryClient = useQueryClient();
  const decide = useRecordMcpApprovalDecisionMutation();

  const submit = async (decision: "approved" | "denied") => {
    try {
      await decide.mutateAsync({
        request: {
          gramProject: projectSlug,
          recordDecisionRequestBody: {
            id: requestId,
            decision,
            rationale: rationale.trim(),
          },
        },
      });
    } catch {
      // The typed rationale is deliberately kept: a failed submit must not
      // cost the reviewer their writing.
      toast.error("Recording the decision failed — nothing was saved");
      return;
    }
    await Promise.all([
      invalidateGetMcpApprovalRequest(queryClient, [{ id: requestId }]),
      invalidateAllListMcpApprovalRequests(queryClient),
    ]);
    setRationale("");
    toast.success(
      decision === "approved" ? "Request approved" : "Request denied",
    );
  };

  const rationaleMissing = rationale.trim().length === 0;

  return (
    <RequireScope scope="mcp_approval:decide" level="component">
      {/* A compose card, not a form: borderless writing surface on a single
          hairline card, actions tucked into a divided footer. */}
      <div className="border-border bg-card border">
        <TextArea
          value={rationale}
          onChange={setRationale}
          placeholder="Why?"
          rows={4}
          className="resize-none border-0 px-3 py-2 text-sm focus:outline-none"
        />
        <div className="border-border flex items-center justify-between gap-3 border-t px-3 py-2">
          <p className="text-muted-foreground text-xs">
            Shared with the requester. In preview, decisions are recorded, not
            enforced.
          </p>
          <div className="flex shrink-0 items-center gap-2">
            <Button
              variant="secondary"
              size="sm"
              className="text-default-destructive border-destructive-default hover:border-destructive-highlight hover:text-default-destructive"
              disabled={rationaleMissing || decide.isPending}
              onClick={() => void submit("denied")}
            >
              <Button.Text>Deny</Button.Text>
            </Button>
            <Button
              variant="secondary"
              size="sm"
              className="text-default-success border-success-default hover:border-success-highlight hover:text-default-success"
              disabled={rationaleMissing || decide.isPending}
              onClick={() => void submit("approved")}
            >
              {decide.isPending && (
                <Button.LeftIcon>
                  <Loader2 className="size-4 animate-spin" />
                </Button.LeftIcon>
              )}
              <Button.Text>Approve</Button.Text>
            </Button>
          </div>
        </div>
      </div>
    </RequireScope>
  );
}
