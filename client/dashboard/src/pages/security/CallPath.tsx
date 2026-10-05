import { cn } from "@/lib/utils";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { X } from "lucide-react";
import { callOutcome, callPathFor, type CallPathLink } from "./call-path";
import { DRAWER_CELL_LABEL } from "./finding-drawer-parts";
import { SEVERITY_EDGE } from "./risk-severity";
import type { SeverityRating } from "./risk-utils";

function PathNode({
  eyebrow,
  name,
  sub,
  className,
}: {
  eyebrow: string;
  name: string;
  sub: string;
  className?: string;
}): JSX.Element {
  return (
    <div
      className={cn(
        "flex min-w-0 flex-col gap-1 border px-3 py-2.5",
        className,
      )}
    >
      <span className={DRAWER_CELL_LABEL}>{eyebrow}</span>
      <span className="truncate text-[13px] font-normal" title={name}>
        {name}
      </span>
      <span
        className="text-muted-foreground truncate font-mono text-[11px]"
        title={sub}
      >
        {sub}
      </span>
    </div>
  );
}

// A blocked link draws solid toward the side the traffic came from, a stop
// marker, then dashed toward where it never arrived.
function PathConnector({
  link,
  blockedFrom,
}: {
  link: CallPathLink;
  blockedFrom: "left" | "right";
}): JSX.Element {
  const solid = <span className="bg-foreground h-px flex-1" />;
  const dashed = (
    <span className="border-neutral-default flex-1 border-t border-dashed" />
  );
  return (
    <div className="flex flex-col items-center gap-1.5 px-1.5">
      <div className="flex w-full items-center">
        {link.blocked ? (
          <>
            {blockedFrom === "left" ? solid : dashed}
            <span className="bg-destructive inline-flex size-[18px] shrink-0 items-center justify-center text-white">
              <X className="size-3" strokeWidth={2.5} />
            </span>
            {blockedFrom === "left" ? dashed : solid}
          </>
        ) : (
          solid
        )}
      </div>
      <span
        className={cn(
          "text-center font-mono text-[10px] tracking-[0.08em] uppercase",
          link.blocked ? "text-destructive" : "text-muted-foreground",
        )}
      >
        {link.label}
      </span>
    </div>
  );
}

export function CallPath({
  result,
  siblings,
  serverName,
  surfaceLabel,
  rating,
}: {
  result: RiskResult;
  siblings: RiskResult[];
  serverName: string;
  surfaceLabel: string;
  rating: SeverityRating | null;
}): JSX.Element {
  const path = callPathFor(
    result.phase,
    callOutcome(result, siblings),
    serverName,
  );
  const response = result.phase === "response";
  return (
    <div className="bg-card border p-4">
      <div className="grid grid-cols-[minmax(0,1fr)_104px_minmax(0,1fr)_104px_minmax(0,1fr)] items-center">
        <PathNode
          eyebrow="Client"
          name={result.userId ?? "Unknown user"}
          sub="MCP client"
        />
        {/* A request flows left to right; a response's traffic comes from
            the server on the right. */}
        <PathConnector
          link={path.left}
          blockedFrom={response ? "right" : "left"}
        />
        <PathNode
          eyebrow="Speakeasy gateway"
          name={surfaceLabel}
          sub={`Scanned ${response ? "response" : "request"}`}
          className={cn(
            "border-foreground border-l-2",
            rating ? SEVERITY_EDGE[rating] : "border-l-foreground",
          )}
        />
        <PathConnector
          link={path.right}
          blockedFrom={response ? "right" : "left"}
        />
        <PathNode
          eyebrow="MCP server"
          name={serverName}
          sub={result.toolName ?? "-"}
          className={cn(
            path.serverReached
              ? "border-foreground"
              : "border-border opacity-55",
          )}
        />
      </div>
      <p className="text-muted-foreground mt-3.5 text-[13px]">{path.caption}</p>
    </div>
  );
}
