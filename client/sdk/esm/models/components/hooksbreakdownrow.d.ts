import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Cross-dimensional aggregation row: one entry per unique (user, server, hook_source, tool) combination
 */
export type HooksBreakdownRow = {
  /**
   * Total events for this combination
   */
  eventCount: number;
  /**
   * Number of failures for this combination
   */
  failureCount: number;
  /**
   * Hook source (e.g. claude-desktop, cursor)
   */
  hookSource: string;
  /**
   * Server name ('local' for non-MCP tools)
   */
  serverName: string;
  /**
   * Tool name
   */
  toolName: string;
  /**
   * User email address
   */
  userEmail: string;
};
/** @internal */
export declare const HooksBreakdownRow$inboundSchema: z.ZodMiniType<
  HooksBreakdownRow,
  unknown
>;
export declare function hooksBreakdownRowFromJSON(
  jsonString: string,
): SafeParseResult<HooksBreakdownRow, SDKValidationError>;
//# sourceMappingURL=hooksbreakdownrow.d.ts.map
