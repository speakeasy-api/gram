import * as z from "zod/v3";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Text format type
 */
export type AgentTextFormat = {
  /**
   * The type of text format (e.g., 'text')
   */
  type: string;
};
/** @internal */
export declare const AgentTextFormat$inboundSchema: z.ZodType<
  AgentTextFormat,
  z.ZodTypeDef,
  unknown
>;
export declare function agentTextFormatFromJSON(
  jsonString: string,
): SafeParseResult<AgentTextFormat, SDKValidationError>;
//# sourceMappingURL=agenttextformat.d.ts.map
