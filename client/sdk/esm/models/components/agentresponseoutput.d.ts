import * as z from "zod/v3";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { AgentResponseText } from "./agentresponsetext.js";
/**
 * Status of the response
 */
export declare const Status: {
  readonly InProgress: "in_progress";
  readonly Completed: "completed";
  readonly Failed: "failed";
};
/**
 * Status of the response
 */
export type Status = ClosedEnum<typeof Status>;
/**
 * Response output from an agent workflow
 */
export type AgentResponseOutput = {
  /**
   * Unix timestamp when the response was created
   */
  createdAt: number;
  /**
   * Error message if the response failed
   */
  error?: string | undefined;
  /**
   * Unique identifier for this response
   */
  id: string;
  /**
   * The instructions that were used
   */
  instructions?: string | undefined;
  /**
   * The model that was used
   */
  model: string;
  /**
   * Object type, always 'response'
   */
  object: string;
  /**
   * Array of output items (messages, tool calls)
   */
  output: Array<any>;
  /**
   * ID of the previous response if continuing
   */
  previousResponseId?: string | undefined;
  /**
   * The final text result from the agent
   */
  result: string;
  /**
   * Status of the response
   */
  status: Status;
  /**
   * Temperature that was used
   */
  temperature: number;
  /**
   * Text format configuration for the response
   */
  text: AgentResponseText;
};
/** @internal */
export declare const Status$inboundSchema: z.ZodNativeEnum<typeof Status>;
/** @internal */
export declare const AgentResponseOutput$inboundSchema: z.ZodType<
  AgentResponseOutput,
  z.ZodTypeDef,
  unknown
>;
export declare function agentResponseOutputFromJSON(
  jsonString: string,
): SafeParseResult<AgentResponseOutput, SDKValidationError>;
//# sourceMappingURL=agentresponseoutput.d.ts.map
