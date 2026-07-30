import * as z from "zod/v3";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { AgentTextFormat } from "./agenttextformat.js";
/**
 * Text format configuration for the response
 */
export type AgentResponseText = {
  /**
   * Text format type
   */
  format: AgentTextFormat;
};
/** @internal */
export declare const AgentResponseText$inboundSchema: z.ZodType<
  AgentResponseText,
  z.ZodTypeDef,
  unknown
>;
export declare function agentResponseTextFromJSON(
  jsonString: string,
): SafeParseResult<AgentResponseText, SDKValidationError>;
//# sourceMappingURL=agentresponsetext.d.ts.map
