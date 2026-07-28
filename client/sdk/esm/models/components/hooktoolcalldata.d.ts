import * as z from "zod/v4-mini";
/**
 * Tool call feature payload.
 */
export type HookToolCallData = {
  /**
   * Tool execution duration in milliseconds, when reported.
   */
  durationMs?: number | undefined;
  /**
   * Tool error payload.
   */
  error?: any | undefined;
  /**
   * Provider tool call identifier.
   */
  id?: string | undefined;
  /**
   * Tool input payload.
   */
  input?: any | undefined;
  /**
   * Whether the failure was caused by user interruption.
   */
  isInterrupt?: boolean | undefined;
  /**
   * Tool name.
   */
  name?: string | undefined;
  /**
   * Tool output payload.
   */
  output?: any | undefined;
  /**
   * Permission type requested by the agent, when applicable.
   */
  permissionType?: string | undefined;
  /**
   * Provider-reported tool call status, when available.
   */
  status?: string | undefined;
};
/** @internal */
export type HookToolCallData$Outbound = {
  duration_ms?: number | undefined;
  error?: any | undefined;
  id?: string | undefined;
  input?: any | undefined;
  is_interrupt?: boolean | undefined;
  name?: string | undefined;
  output?: any | undefined;
  permission_type?: string | undefined;
  status?: string | undefined;
};
/** @internal */
export declare const HookToolCallData$outboundSchema: z.ZodMiniType<
  HookToolCallData$Outbound,
  HookToolCallData
>;
export declare function hookToolCallDataToJSON(
  hookToolCallData: HookToolCallData,
): string;
//# sourceMappingURL=hooktoolcalldata.d.ts.map
