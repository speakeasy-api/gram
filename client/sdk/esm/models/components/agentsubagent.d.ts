import * as z from "zod/v3";
import { AgentToolset, AgentToolset$Outbound } from "./agenttoolset.js";
/**
 * A sub-agent definition for the agent workflow
 */
export type AgentSubAgent = {
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
  toolsets?: Array<AgentToolset> | undefined;
};
/** @internal */
export type AgentSubAgent$Outbound = {
  description: string;
  environment_slug?: string | undefined;
  instructions?: string | undefined;
  name: string;
  tools?: Array<string> | undefined;
  toolsets?: Array<AgentToolset$Outbound> | undefined;
};
/** @internal */
export declare const AgentSubAgent$outboundSchema: z.ZodType<
  AgentSubAgent$Outbound,
  z.ZodTypeDef,
  AgentSubAgent
>;
export declare function agentSubAgentToJSON(
  agentSubAgent: AgentSubAgent,
): string;
//# sourceMappingURL=agentsubagent.d.ts.map
