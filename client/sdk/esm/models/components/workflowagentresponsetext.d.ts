import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { WorkflowAgentTextFormat } from "./workflowagenttextformat.js";
/**
 * Text format configuration for the response
 */
export type WorkflowAgentResponseText = {
  /**
   * Text format type
   */
  format: WorkflowAgentTextFormat;
};
/** @internal */
export declare const WorkflowAgentResponseText$inboundSchema: z.ZodMiniType<
  WorkflowAgentResponseText,
  unknown
>;
export declare function workflowAgentResponseTextFromJSON(
  jsonString: string,
): SafeParseResult<WorkflowAgentResponseText, SDKValidationError>;
//# sourceMappingURL=workflowagentresponsetext.d.ts.map
