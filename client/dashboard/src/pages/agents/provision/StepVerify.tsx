import { Text } from "@/components/ui/Text";
import { dateTimeFormatters } from "@/lib/dates";
import { cn } from "@/lib/utils";
import type { JSX, ReactNode } from "react";
import type { AgentPurpose } from "./device-agent";

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

type VerifyCopy = {
  firstEvent: string;
  waiting: string;
  waitingDetail: string;
  connected: string;
  checks: { title: string; description: string }[];
  troubleshooting: ReactNode[];
};

function verifyCopy(purpose: AgentPurpose, gatewayURL: string): VerifyCopy {
  switch (purpose) {
    case "device-agent":
      return {
        firstEvent: "First checked in",
        waiting: "Waiting for the device agent to check in",
        waitingDetail:
          "Keep this page open while the setup runs. This updates when the device agent syncs; a sync in the first minute after the command was generated may not show.",
        connected: "Its key was accepted.",
        checks: [
          {
            title: "Device agent installed",
            description: "The setup script installed and enrolled the agent.",
          },
          {
            title: "Key accepted",
            description:
              "The device agent synced with Gram using the agent's key.",
          },
        ],
        troubleshooting: [
          <>
            <code className="text-xs">403</code> — the key cannot sync. Check
            the agent still holds{" "}
            <code className="text-xs">org:device_agent_sync</code>.
          </>,
          <>
            Persistent installs —{" "}
            <code className="text-xs">systemctl --user status speakeasyd</code>{" "}
            shows whether the service is running.
          </>,
          "No check-in at all — the setup command was not run, or its code had already been used. Regenerate it.",
        ],
      };
    case "mcp":
      return {
        firstEvent: "First call",
        waiting: "Waiting for the first call",
        waitingDetail:
          "Keep this page open while the agent runs its setup. This updates as calls reach the gateway.",
        connected: "The agent is live.",
        checks: [
          {
            title: "Credential accepted",
            description:
              "The API key was presented to the agent gateway and admitted.",
          },
          {
            title: "Gateway reachable",
            description: `The runtime resolved ${gatewayURL} and completed a request.`,
          },
        ],
        troubleshooting: [
          <>
            <code className="text-xs">401</code> — the credential was rejected.
            Run the setup command again; a code is spent on first use.
          </>,
          <>
            <code className="text-xs">403</code> — the tool is not granted.
            Check the server selection and the agent&apos;s permissions.
          </>,
          "No requests at all — the runtime is not pointed at the gateway URL.",
        ],
      };
  }
}

export function StepVerify({
  state,
  firstCallAt,
  gatewayURL,
  purpose = "mcp",
}: {
  state: VerifyState;
  firstCallAt?: Date;
  gatewayURL: string;
  purpose?: AgentPurpose;
}): JSX.Element {
  const copy = verifyCopy(purpose, gatewayURL);
  const checks = copy.checks.map((check) => ({
    ...check,
    done: state === "connected",
  }));

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
            {state === "connected" ? "Connected" : copy.waiting}
          </h2>
          <Text muted small>
            {state === "connected"
              ? `${copy.firstEvent} ${firstCallAt ? dateTimeFormatters.full.format(firstCallAt) : "received"}. ${copy.connected}`
              : copy.waitingDetail}
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
          {copy.troubleshooting.map((item, index) => (
            // Static copy in a fixed order.
            <li key={index}>{item}</li>
          ))}
        </ul>
      </div>
    </div>
  );
}
