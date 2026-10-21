import { Text } from "@/components/ui/Text";
import { dateTimeFormatters } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { useWorkloadDeviceJoinTokens } from "@gram/client/react-query/workloadDeviceJoinTokens.js";
import type { JSX } from "react";
import { useAgentAdmissions } from "../agentAdmissions";
import { WizardStepHeader } from "../provision/WizardChrome";
import {
  controlPlaneURL,
  redeemedToken,
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
  const tokens = useWorkloadDeviceJoinTokens({ agentId }, undefined, {
    enabled: watched.length > 0,
    throwOnError: false,
    refetchInterval: (query) =>
      redeemedToken(query.state.data?.result.items ?? [], watched)
        ? false
        : pollMs,
  });
  const redeemed = redeemedToken(tokens.data?.result.items ?? [], watched);
  // The device's admission carries the hostname it reported, which is what a
  // reader recognizes it by.
  const admissions = useAgentAdmissions(redeemed ? agentId : "");
  const device = redeemed
    ? admissions.admissions.find(
        (admission) => admission.subject === redeemed.consumedBySubject,
      )
    : undefined;

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
            redeemed
              ? "bg-default-success"
              : "bg-warning-default animate-pulse",
          )}
        />
        <div className="min-w-0 space-y-1">
          <Text className="font-medium">
            {redeemed ? "Device enrolled" : "Waiting for the box to enroll"}
          </Text>
          <Text muted small>
            {redeemed
              ? `${device?.name || "A device"} enrolled ${redeemed.consumedAt ? dateTimeFormatters.humanize(redeemed.consumedAt) : "just now"}. It acts as this agent from now on.`
              : watched.length === 0
                ? "Create an install command or a bootstrap token on the previous step, then run it on the box."
                : "This updates once the box redeems its bootstrap token."}
          </Text>
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
      {!redeemed && (
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
