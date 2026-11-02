import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import {
  Bot,
  ChevronDown,
  ChevronRight,
  Layers,
  Server,
  type LucideIcon,
} from "lucide-react";
import type { JSX } from "react";

/** The two sign-ins between an agent and an MCP server. */
export type SignInHop = "agent" | "mcp";

const HOPS: Record<SignInHop, { label: string; caption: string }> = {
  agent: {
    label: "Agent sign-in",
    caption:
      "This only gets the agent into Speakeasy. It has nothing to do with your MCP servers: Speakeasy signs in to each of them the way you set on MCP sign-in.",
  },
  mcp: {
    label: "MCP sign-in",
    caption:
      "This is separate from how the agent signs in to Speakeasy, which you choose next, on Agent sign-in.",
  },
};

/**
 * Two sign-ins stand between an agent and an MCP server, and each has a step
 * of its own. The path lights the one a step sets, so a credential that only
 * gets the agent into Speakeasy is never read as one for the servers behind
 * it. Laid out like a run graph: solid cards for the parties, arrows for the
 * sign-ins, across the page and down it at phone width.
 */
export function SignInPath({ hop }: { hop: SignInHop }): JSX.Element {
  return (
    <figure
      aria-label={`${HOPS[hop].label}: ${hop === "agent" ? "agent to Speakeasy" : "Speakeasy to MCP servers"}`}
      className="border-border bg-muted/30 border p-4 sm:p-5"
    >
      <div className="flex flex-col sm:flex-row sm:items-center">
        <Party
          icon={Bot}
          name="Agent"
          role="Your runtime"
          on={hop === "agent"}
        />
        <Hop label={HOPS.agent.label} on={hop === "agent"} />
        <Party icon={Layers} name="Speakeasy" role="Control plane" on />
        <Hop label={HOPS.mcp.label} on={hop === "mcp"} />
        <Party
          icon={Server}
          name="MCP servers"
          role="Upstream"
          on={hop === "mcp"}
        />
      </div>
      <figcaption className="border-border mt-4 border-t pt-3">
        <Text muted small>
          {HOPS[hop].caption}
        </Text>
      </figcaption>
    </figure>
  );
}

function Party({
  icon: Icon,
  name,
  role,
  on,
}: {
  icon: LucideIcon;
  name: string;
  role: string;
  on: boolean;
}): JSX.Element {
  return (
    <div
      className={cn(
        "bg-background flex shrink-0 items-center gap-2.5 border px-3 py-2.5",
        on ? "border-information-default" : "border-border",
      )}
    >
      <Icon
        className={cn("size-4 shrink-0", !on && "text-muted-foreground")}
        aria-hidden="true"
      />
      <span className="min-w-0">
        <span
          className={cn(
            "block text-sm leading-tight",
            !on && "text-muted-foreground",
          )}
        >
          {name}
        </span>
        <span className="text-muted-foreground block font-mono text-[10px] tracking-[0.08em] uppercase">
          {role}
        </span>
      </span>
    </div>
  );
}

/**
 * One sign-in as an arrow: solid, breathing and marked as this step when lit,
 * dashed otherwise. Across the page between the cards, or down the page at
 * phone width, where the label sits beside it.
 */
function Hop({ label, on }: { label: string; on: boolean }): JSX.Element {
  const name = (
    <span
      className={cn(
        "font-mono text-[10px] tracking-[0.08em] whitespace-nowrap uppercase",
        on ? "text-foreground" : "text-muted-foreground",
      )}
    >
      {label}
    </span>
  );
  const badge = on && (
    <span className="bg-information-default px-1.5 py-0.5 font-mono text-[10px] tracking-[0.08em] whitespace-nowrap text-white uppercase">
      This step
    </span>
  );
  const shaft = on ? "bg-information-default" : "border-border border-dashed";
  // The arrowheads are strokes: lucide draws them in currentColor, which a
  // stroke class overrides.
  const head = cn(
    "size-4 shrink-0",
    on ? "stroke-information-default" : "text-muted-foreground/60",
  );
  return (
    <>
      <div className="flex min-w-24 flex-1 flex-col items-center gap-1.5 px-3 max-sm:hidden">
        {name}
        <span
          aria-hidden="true"
          className={cn(
            "flex w-full items-center",
            on && "motion-safe:animate-sign-in-breathe",
          )}
        >
          <span
            className={cn(
              "flex-1",
              on ? `h-0.5 ${shaft}` : `border-t ${shaft}`,
            )}
          />
          <ChevronRight className={cn("-ml-2", head)} strokeWidth={2.5} />
        </span>
        {badge || <span className="h-[18px]" aria-hidden="true" />}
      </div>
      <div className="flex items-center gap-3 py-1 pl-4 sm:hidden">
        <span
          aria-hidden="true"
          className={cn(
            "flex h-10 flex-col items-center",
            on && "motion-safe:animate-sign-in-breathe",
          )}
        >
          <span
            className={cn(
              "flex-1",
              on ? `w-0.5 ${shaft}` : `border-l ${shaft}`,
            )}
          />
          <ChevronDown className={cn("-mt-2", head)} strokeWidth={2.5} />
        </span>
        {name}
        {badge}
      </div>
    </>
  );
}
