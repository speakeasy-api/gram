import * as z from "zod/v4-mini";
/**
 * A toolset reference for agent execution
 */
export type WorkflowAgentToolset = {
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
export type WorkflowAgentToolset$Outbound = {
  environment_slug: string;
  toolset_slug: string;
};
/** @internal */
export declare const WorkflowAgentToolset$outboundSchema: z.ZodMiniType<
  WorkflowAgentToolset$Outbound,
  WorkflowAgentToolset
>;
export declare function workflowAgentToolsetToJSON(
  workflowAgentToolset: WorkflowAgentToolset,
): string;
//# sourceMappingURL=workflowagenttoolset.d.ts.map
