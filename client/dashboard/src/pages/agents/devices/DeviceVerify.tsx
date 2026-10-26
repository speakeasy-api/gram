import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { dateTimeFormatters } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { useGramContext } from "@gram/client/react-query/_context.js";
import {
  buildWorkloadDeviceJoinTokensQuery,
  type WorkloadDeviceJoinTokensQueryData,
} from "@gram/client/react-query/workloadDeviceJoinTokens.js";
import {
  queryOptions,
  useQueries,
  useQueryClient,
} from "@tanstack/react-query";
import { useEffect, useState, type JSX } from "react";
import { useAgentAdmissions } from "../agentAdmissions";
import { WizardStepHeader } from "../credentials/WizardChrome";
import {
  controlPlaneURL,
  redeemedToken,
  watchedTokensFinished,
  type WatchedToken,
} from "./device-enrollment";

/**
 * Whether the box this flow set up has enrolled: a bootstrap token it created
 * shows as redeemed, naming the device that spent it.
 *
 * Polls while waiting and stops once a device has enrolled. React Query does
 * not refetch on an interval while the tab is hidden, so a forgotten tab
 * stops asking.
 */
export function DeviceVerify({
  agentId,
  watched,
  pollMs = 5000,
}: {
  agentId: string;
  watched: WatchedToken[];
  pollMs?: number;
}): JSX.Element {
  const client = useGramContext();
  const queryClient = useQueryClient();
  const [now, setNow] = useState(() => Date.now());
  const definitions = watched.map((entry) => ({
    entry,
    options: buildWorkloadDeviceJoinTokensQuery(client, {
      agentId,
      tokenId: entry.id,
    }),
  }));
  const queries = useQueries({
    queries: definitions.map(({ entry, options }) =>
      queryOptions({
        ...options,
        throwOnError: false,
        refetchInterval: (query) => {
          const cached = definitions.flatMap(
            ({ options }) =>
              queryClient.getQueryData<WorkloadDeviceJoinTokensQueryData>(
                options.queryKey,
              )?.result.items ?? [],
          );
          if (redeemedToken(cached, watched)) return false;
          return watchedTokensFinished(query.state.data?.result.items ?? [], [
            entry,
          ])
            ? false
            : pollMs;
        },
      }),
    ),
  });
  const items = queries.flatMap((query) => query.data?.result.items ?? []);
  const redeemed = redeemedToken(items, watched);
  const finished = watchedTokensFinished(items, watched, now);
  const isError = queries.some((query) => query.isError);
  useEffect(() => {
    if (redeemed || finished || watched.length === 0) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [redeemed, finished, watched.length]);
  let title = "Waiting for the box to enroll";
  let description = "This updates once the box redeems its bootstrap token.";
  if (watched.length === 0)
    description =
      "Create an install command or a bootstrap token on the previous step, then run it on the box.";
  if (finished) {
    title = "Bootstrap token no longer active";
    description =
      "The token expired or was revoked. Return to the previous step to create a new one.";
  }
  if (isError) {
    title = "Could not check device enrollment";
    description = "Try again to see whether the box enrolled.";
  }
  // The device's admission carries the hostname it reported, which is what a
  // reader recognizes it by.
  const admissions = useAgentAdmissions(redeemed ? agentId : "");
  const device = redeemed
    ? admissions.admissions.find(
        (admission) => admission.subject === redeemed.consumedBySubject,
      )
    : undefined;

  if (redeemed) {
    title = "Device enrolled";
    const enrolledAt = redeemed.consumedAt
      ? dateTimeFormatters.humanize(redeemed.consumedAt)
      : "just now";
    description = `${device?.name || "A device"} enrolled ${enrolledAt}. It acts as this agent from now on.`;
  }

  return (
    <div className="space-y-5">
      <WizardStepHeader
        title="Verify"
        description="Wait here while the box enrolls."
      />
      <div className="border-border flex items-start gap-3 border p-4">
        <span
          aria-hidden="true"
          className={cn(
            "mt-1.5 size-2 shrink-0 rounded-full",
            redeemed ? "bg-default-success" : "bg-warning-default",
          )}
        />
        <div className="min-w-0 space-y-1">
          <Text className="font-medium">{title}</Text>
          <Text muted small>
            {description}
          </Text>
          {isError && !redeemed && (
            <Button
              variant="secondary"
              size="sm"
              onClick={() =>
                void Promise.all(queries.map((query) => query.refetch()))
              }
            >
              Try again
            </Button>
          )}
          {redeemed?.consumedBySubject && (
            <details>
              <summary className="text-muted-foreground hover:text-foreground cursor-pointer text-xs">
                Technical details
              </summary>
              <Text muted small className="mt-1 font-mono break-all">
                Subject {redeemed.consumedBySubject}
              </Text>
            </details>
          )}
        </div>
      </div>
      {!redeemed && !finished && !isError && (
        <div className="space-y-2">
          <span className="text-eyebrow">If nothing arrives</span>
          <ul className="space-y-1 text-sm">
            <li>
              The token expired before the box redeemed it. Create a new one.
            </li>
            <li>
              The box cannot reach {new URL(controlPlaneURL()).host}. Check its
              outbound HTTPS.
            </li>
            <li>The device service is not running on the box.</li>
          </ul>
        </div>
      )}
    </div>
  );
}
