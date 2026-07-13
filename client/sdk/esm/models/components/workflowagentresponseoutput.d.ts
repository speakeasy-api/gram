import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { WorkflowAgentResponseText } from "./workflowagentresponsetext.js";
/**
 * Status of the response
 */
export declare const WorkflowAgentResponseOutputStatus: {
  readonly InProgress: "in_progress";
  readonly Completed: "completed";
  readonly Failed: "failed";
};
/**
 * Status of the response
 */
export type WorkflowAgentResponseOutputStatus = ClosedEnum<
  typeof WorkflowAgentResponseOutputStatus
>;
/**
 * Response output from an agent workflow
 */
export type WorkflowAgentResponseOutput = {
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
  status: WorkflowAgentResponseOutputStatus;
  /**
   * Temperature that was used
   */
  temperature: number;
  /**
   * Text format configuration for the response
   */
  text: WorkflowAgentResponseText;
};
/** @internal */
export declare const WorkflowAgentResponseOutputStatus$inboundSchema: z.ZodMiniEnum<
  typeof WorkflowAgentResponseOutputStatus
>;
/** @internal */
export declare const WorkflowAgentResponseOutput$inboundSchema: z.ZodMiniType<
  WorkflowAgentResponseOutput,
  unknown
>;
export declare function workflowAgentResponseOutputFromJSON(
  jsonString: string,
): SafeParseResult<WorkflowAgentResponseOutput, SDKValidationError>;
//# sourceMappingURL=workflowagentresponseoutput.d.ts.map
