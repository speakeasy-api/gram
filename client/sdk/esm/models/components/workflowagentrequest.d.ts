import * as z from "zod/v4-mini";
import {
  WorkflowAgentToolset,
  WorkflowAgentToolset$Outbound,
} from "./workflowagenttoolset.js";
import {
  WorkflowSubAgent,
  WorkflowSubAgent$Outbound,
} from "./workflowsubagent.js";
/**
 * Request payload for creating an agent response
 */
export type WorkflowAgentRequest = {
  /**
   * If true, returns immediately with a response ID for polling
   */
  async?: boolean | undefined;
  /**
   * The input to the agent - can be a string or array of messages
   */
  input: any;
  /**
   * System instructions for the agent
   */
  instructions?: string | undefined;
  /**
   * The model to use for the agent (e.g., openai/gpt-4o)
   */
  model: string;
  /**
   * ID of a previous response to continue from
   */
  previousResponseId?: string | undefined;
  /**
   * If true, stores the response defaults to true
   */
  store?: boolean | undefined;
  /**
   * Sub-agents available for delegation
   */
  subAgents?: Array<WorkflowSubAgent> | undefined;
  /**
   * Temperature for model responses
   */
  temperature?: number | undefined;
  /**
   * Toolsets available to the agent
   */
  toolsets?: Array<WorkflowAgentToolset> | undefined;
};
/** @internal */
export type WorkflowAgentRequest$Outbound = {
  async?: boolean | undefined;
  input: any;
  instructions?: string | undefined;
  model: string;
  previous_response_id?: string | undefined;
  store?: boolean | undefined;
  sub_agents?: Array<WorkflowSubAgent$Outbound> | undefined;
  temperature?: number | undefined;
  toolsets?: Array<WorkflowAgentToolset$Outbound> | undefined;
};
/** @internal */
export declare const WorkflowAgentRequest$outboundSchema: z.ZodMiniType<
  WorkflowAgentRequest$Outbound,
  WorkflowAgentRequest
>;
export declare function workflowAgentRequestToJSON(
  workflowAgentRequest: WorkflowAgentRequest,
): string;
//# sourceMappingURL=workflowagentrequest.d.ts.map
