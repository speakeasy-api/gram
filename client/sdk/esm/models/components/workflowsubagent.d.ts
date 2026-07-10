import * as z from "zod/v4-mini";
import { WorkflowAgentToolset, WorkflowAgentToolset$Outbound } from "./workflowagenttoolset.js";
/**
 * A sub-agent definition for the agent workflow
 */
export type WorkflowSubAgent = {
    /**
     * Description of what this sub-agent does
     */
    description: string;
    /**
     * The environment slug for auth
     */
    environmentSlug?: string | undefined;
    /**
     * Instructions for this sub-agent
     */
    instructions?: string | undefined;
    /**
     * The name of this sub-agent
     */
    name: string;
    /**
     * Tool URNs available to this sub-agent
     */
    tools?: Array<string> | undefined;
    /**
     * Toolsets available to this sub-agent
     */
    toolsets?: Array<WorkflowAgentToolset> | undefined;
};
/** @internal */
export type WorkflowSubAgent$Outbound = {
    description: string;
    environment_slug?: string | undefined;
    instructions?: string | undefined;
    name: string;
    tools?: Array<string> | undefined;
    toolsets?: Array<WorkflowAgentToolset$Outbound> | undefined;
};
/** @internal */
export declare const WorkflowSubAgent$outboundSchema: z.ZodMiniType<WorkflowSubAgent$Outbound, WorkflowSubAgent>;
export declare function workflowSubAgentToJSON(workflowSubAgent: WorkflowSubAgent): string;
//# sourceMappingURL=workflowsubagent.d.ts.map