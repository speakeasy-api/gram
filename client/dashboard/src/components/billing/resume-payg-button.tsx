import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { invalidateAllGetStripeSubscription } from "@gram/client/react-query/getStripeSubscription.js";
import { useResumeStripeSubscriptionMutation } from "@gram/client/react-query/resumeStripeSubscription.js";
import { useQueryClient } from "@tanstack/react-query";

const BUTTON_LABEL = "Resume pay as you go";
const ERROR_MESSAGE = "Couldn't resume the subscription. Try again.";

/**
 * Clears a scheduled end-of-period cancellation.
 *
 * Resuming restores the plan the organization already had, so it goes through
 * without a confirmation — the destructive direction is the one that asks.
 */
export function ResumePaygButton(): JSX.Element {
  const queryClient = useQueryClient();

  const resume = useResumeStripeSubscriptionMutation({
    // Settled, not success: Stripe can clear the scheduled cancellation and
    // the request still fail afterwards, so a failed call is no proof the
    // subscription is unchanged. Refetching either way keeps the dashboard
    // from showing a plan state Stripe has already moved on from.
    onSettled: () => {
      // Every billing surface reads the subscription off this key, so the
      // whole key is refreshed — the scheduled cancellation has to disappear
      // from the plan state and from anything gating on it.
      void invalidateAllGetStripeSubscription(queryClient);
    },
  });

  return (
    <Stack gap={2} align="start">
      <Button
        variant="primary"
        onClick={() => resume.mutate({})}
        disabled={resume.isPending}
      >
        {resume.isPending ? "RESUMING..." : BUTTON_LABEL}
      </Button>
      {resume.isError && (
        <Text small destructive role="alert">
          {ERROR_MESSAGE}
        </Text>
      )}
    </Stack>
  );
}
