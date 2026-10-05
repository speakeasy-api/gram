import { Text } from "@/components/ui/Text";
import { dateTimeFormatters } from "@/lib/dates";
import { cn } from "@/lib/utils";
import type { JSX } from "react";

/**
 * Whether the thing we just provisioned actually works. The page watches the
 * key itself: a key that has been accessed is a runtime that reached the
 * gateway and was admitted, which is the one fact that separates "configured"
 * from "connected".
 *
 * Only checks the platform can observe appear here. A row that always said
 * "pending" would teach people to ignore the list.
 */

export type VerifyState = "waiting" | "connected" | "unknown";

export function StepVerify({
  state,
  firstCallAt,
  gatewayURL,
}: {
  state: VerifyState;
  firstCallAt?: Date;
  gatewayURL: string;
}): JSX.Element {
  const checks: { title: string; description: string; done: boolean }[] = [
    {
      title: "Credential accepted",
      description:
        "The API key was presented to the agent gateway and admitted.",
      done: state === "connected",
    },
    {
      title: "Gateway reachable",
      description: `The runtime resolved ${gatewayURL} and completed a request.`,
      done: state === "connected",
    },
  ];

  return (
    <div className="space-y-5">
      <div className="flex items-start justify-between gap-6">
        <div className="space-y-1">
          <h2 className="flex items-center gap-2 text-lg font-semibold">
            <span
              aria-hidden="true"
              className={cn(
                "size-2 rounded-full",
                state === "connected"
                  ? "bg-default-success"
                  : "bg-muted-foreground",
              )}
            />
            {state === "connected" ? "Connected" : "Waiting for the first call"}
          </h2>
          <Text muted small>
            {state === "connected"
              ? `First call ${firstCallAt ? dateTimeFormatters.full.format(firstCallAt) : "received"}. The agent is live.`
              : "Keep this page open while the agent runs its setup. This updates as calls reach the gateway."}
          </Text>
        </div>
      </div>

      <ol className="divide-border border-border divide-y border">
        {checks.map((check, index) => (
          <li
            key={check.title}
            className="flex items-start justify-between gap-6 px-4 py-3"
          >
            <div className="flex gap-3">
              <span className="text-muted-foreground font-mono text-xs">
                {String(index + 1).padStart(2, "0")}
              </span>
              <div>
                <Text className="font-medium">{check.title}</Text>
                <Text muted small>
                  {check.description}
                </Text>
              </div>
            </div>
            <span
              className={cn(
                "shrink-0 font-mono text-[10px] tracking-[0.08em] uppercase",
                check.done ? "text-default-success" : "text-muted-foreground",
              )}
            >
              {check.done ? "Confirmed" : "Waiting"}
            </span>
          </li>
        ))}
      </ol>

      <div className="space-y-2">
        <span className="text-muted-foreground font-mono text-[10px] tracking-[0.08em] uppercase">
          If nothing arrives
        </span>
        <ul className="space-y-1 text-sm">
          <li>
            <code className="text-xs">401</code> — the credential was rejected.
            Run the setup command again; a code is spent on first use.
          </li>
          <li>
            <code className="text-xs">403</code> — the tool is not granted.
            Check the server selection and the agent's permissions.
          </li>
          <li>
            No requests at all — the runtime is not pointed at the gateway URL.
          </li>
        </ul>
      </div>
    </div>
  );
}
