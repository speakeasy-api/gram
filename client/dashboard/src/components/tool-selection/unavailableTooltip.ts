import type { ReactNode } from "react";
import type { ToolSelectionServer } from "./ToolSelectionPanel";

const unproxiedTooltip =
  "Speakeasy doesn't proxy this server's traffic, so its tools can't be permissioned individually.";

/**
 * The unproxied explanation belongs only to rows using the default unproxied
 * label; a row unavailable for another reason (say, no catalog on this
 * surface) keeps just its own label unless it brings its own tooltip.
 */
export function unavailableTooltip(
  server: Pick<ToolSelectionServer, "unavailableLabel" | "unavailableTooltip">,
): ReactNode {
  if (server.unavailableTooltip) return server.unavailableTooltip;
  return server.unavailableLabel ? undefined : unproxiedTooltip;
}
