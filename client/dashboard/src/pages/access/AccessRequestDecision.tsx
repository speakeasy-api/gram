import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Text } from "@/components/ui/Text";
import type { AccessRequest } from "@gram/client/models/components/accessrequest.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { invalidateAllAccessRequests } from "@gram/client/react-query/accessRequests.js";
import { invalidateAllChallengeBuckets } from "@gram/client/react-query/challengeBuckets.js";
import { invalidateAllChallenges } from "@gram/client/react-query/challenges.js";
import { useDecideAccessRequestMutation } from "@gram/client/react-query/decideAccessRequest.js";
import { invalidateAllTemporaryGrants } from "@gram/client/react-query/temporaryGrants.js";
import { useQueryClient } from "@tanstack/react-query";
import { format } from "date-fns";
import { useState } from "react";
import { approvalChoices } from "./approvalChoices";

/**
 * Approve a pending access request for a fixed window, or deny it. Approving
 * gives temporary access that ends on its own; a role assignment below stays
 * the way to give it permanently.
 */
export function AccessRequestDecision({
  request,
  requesterName,
}: {
  request: AccessRequest;
  requesterName: string;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [choice, setChoice] = useState<string | null>(null);
  const decide = useDecideAccessRequestMutation({
    // Refresh on a refusal too: a 409 means the request was decided
    // elsewhere, and the list should show that. An approval also resolves the
    // requester's matching denials, so the challenge lists change with it.
    onSettled: async () => {
      await Promise.all([
        invalidateAllAccessRequests(queryClient),
        invalidateAllTemporaryGrants(queryClient),
        invalidateAllChallenges(queryClient),
        invalidateAllChallengeBuckets(queryClient),
      ]);
    },
  });

  if (decide.isSuccess) {
    const decided = decide.data;
    return (
      <div className="flex items-center gap-1.5 py-1">
        <Icon
          name={decided.status === "approved" ? "check" : "x"}
          className="size-4"
        />
        <Text variant="body" className="text-sm">
          {decided.status === "approved" && decided.grantExpiresAt
            ? `${requesterName} has access until ${format(new Date(decided.grantExpiresAt), "d MMM, HH:mm")}.`
            : "Request denied."}
        </Text>
      </div>
    );
  }

  const submit = (key: string, approve: boolean) => {
    // The window starts when the administrator clicks, not when the dialog
    // opened.
    const expiresAt = approve
      ? approvalChoices(new Date()).find((option) => option.key === key)
          ?.expiresAt
      : undefined;
    if (approve && !expiresAt) return;
    setChoice(key);
    decide.mutate({
      request: {
        decideAccessRequestForm: {
          id: request.id,
          approve,
          expiresAt,
        },
      },
    });
  };

  return (
    <div className="space-y-2">
      <Text className="text-eyebrow">Give temporary access</Text>
      <div className="flex flex-wrap gap-2">
        {approvalChoices(new Date()).map((option) => (
          <Button
            key={option.key}
            size="sm"
            variant={option.key === "1h" ? "primary" : "secondary"}
            disabled={decide.isPending}
            onClick={() => submit(option.key, true)}
          >
            {decide.isPending && choice === option.key
              ? "Approving…"
              : option.label}
          </Button>
        ))}
        <Button
          size="sm"
          variant="destructive-secondary"
          disabled={decide.isPending}
          onClick={() => submit("deny", false)}
        >
          {decide.isPending && choice === "deny" ? "Denying…" : "Deny"}
        </Button>
      </div>
      <Text muted small>
        Access ends on its own. To give it permanently, assign a role below.
      </Text>
      {decide.isError && (
        <Text variant="body" className="text-destructive text-sm">
          {decide.error instanceof GramError && decide.error.statusCode === 409
            ? `${requesterName} already has this access, or the request was decided elsewhere. Check their temporary access.`
            : "The request could not be decided. Try again."}
        </Text>
      )}
    </div>
  );
}
