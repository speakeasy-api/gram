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

interface VerifyCopy {
  checks: { title: string; description: string }[];
  /** What a reader checks when nothing arrives, one cause per line. */
  causes: { code?: string; text: string }[];
}

/**
 * The checks, and the causes of silence, for what the agent calls: the MCP
 * gateway, or for an agent that reaches no server, the hooks endpoint.
 */
function verifyCopy(gatewayURL: string, hooksOnly: boolean): VerifyCopy {
  if (hooksOnly) {
    return {
      checks: [
        {
          title: "Credential accepted",
          description:
            "The API key was presented with a hook event and admitted.",
        },
        {
          title: "Hook events arriving",
          description:
            "The observability plugin sent an event under this agent.",
        },
      ],
      causes: [
        {
          code: "401",
          text: "the key was rejected. Check that the runtime was restarted after the key was set.",
        },
        {
          code: "403",
          text: "the events named another project. Set the project slug to this agent's project.",
        },
        {
          text: "No requests at all: the observability plugin is not installed, or its hooks are not enabled.",
        },
      ],
    };
  }
  return {
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
    causes: [
      {
        code: "401",
        text: "the credential was rejected. Run the setup command again; a code is spent on first use.",
      },
      {
        code: "403",
        text: "the tool is not granted. Check the server selection and the agent's permissions.",
      },
      {
        text: "No requests at all: the runtime is not pointed at the gateway URL.",
      },
    ],
  };
}

export function StepVerify({
  state,
  firstCallAt,
  gatewayURL,
  hooksOnly = false,
}: {
  state: VerifyState;
  firstCallAt?: Date;
  gatewayURL: string;
  /** The agent reaches no MCP server and only sends hook events. */
  hooksOnly?: boolean;
}): JSX.Element {
  const copy = verifyCopy(gatewayURL, hooksOnly);
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
            <div className="flex items-baseline gap-3">
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
            {/* Beside the word rather than above it, so the status reads on
                the same line as the check it belongs to. The dot pulses while
                the check is outstanding and settles green once it passes. */}
            <span className="flex h-5 shrink-0 items-center gap-2">
              <span
                aria-hidden="true"
                className={cn(
                  "size-2 rounded-full",
                  check.done
                    ? "bg-default-success"
                    : "bg-warning-default animate-pulse",
                )}
              />
              <span
                className={cn(
                  "font-mono text-[10px] tracking-[0.08em] uppercase",
                  check.done ? "text-default-success" : "text-muted-foreground",
                )}
              >
                {check.done ? "Confirmed" : "Waiting"}
              </span>
            </span>
          </li>
        ))}
      </ol>

      <div className="space-y-2">
        <span className="text-muted-foreground font-mono text-[10px] tracking-[0.08em] uppercase">
          If nothing arrives
        </span>
        <ul className="space-y-1 text-sm">
          {copy.causes.map((cause) => (
            <li key={cause.text}>
              {cause.code && (
                <>
                  <code className="text-xs">{cause.code}</code>:{" "}
                </>
              )}
              {cause.text}
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
