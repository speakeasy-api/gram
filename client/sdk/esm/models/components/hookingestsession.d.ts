import * as z from "zod/v4-mini";
/**
 * Agent session and turn identity, independent of provider naming.
 */
export type HookIngestSession = {
  /**
   * Current working directory when the event fired.
   */
  cwd?: string | undefined;
  /**
   * Stable conversation or session identifier.
   */
  id?: string | undefined;
  /**
   * Model identifier reported by the local agent.
   */
  model?: string | undefined;
  /**
   * Generation, request, or turn identifier.
   */
  turnId?: string | undefined;
};
/** @internal */
export type HookIngestSession$Outbound = {
  cwd?: string | undefined;
  id?: string | undefined;
  model?: string | undefined;
  turn_id?: string | undefined;
};
/** @internal */
export declare const HookIngestSession$outboundSchema: z.ZodMiniType<
  HookIngestSession$Outbound,
  HookIngestSession
>;
export declare function hookIngestSessionToJSON(
  hookIngestSession: HookIngestSession,
): string;
//# sourceMappingURL=hookingestsession.d.ts.map
