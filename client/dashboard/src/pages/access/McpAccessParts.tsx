import { Network } from "lucide-react";
import type { JSX } from "react";

/**
 * A server's mark: the network glyph MCP server lists fall back to when a
 * server has no logo.
 */
export function ServerMark(): JSX.Element {
  return (
    <Network
      aria-hidden="true"
      className="text-muted-foreground size-5 shrink-0"
    />
  );
}
