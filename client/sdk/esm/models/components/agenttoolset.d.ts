import * as z from "zod/v3";
/**
 * A toolset reference for agent execution
 */
export type AgentToolset = {
  /**
   * The slug of the environment for auth
   */
  environmentSlug: string;
  /**
   * The slug of the toolset to use
   */
  toolsetSlug: string;
};
/** @internal */
export type AgentToolset$Outbound = {
  environment_slug: string;
  toolset_slug: string;
};
/** @internal */
export declare const AgentToolset$outboundSchema: z.ZodType<
  AgentToolset$Outbound,
  z.ZodTypeDef,
  AgentToolset
>;
export declare function agentToolsetToJSON(agentToolset: AgentToolset): string;
//# sourceMappingURL=agenttoolset.d.ts.map
