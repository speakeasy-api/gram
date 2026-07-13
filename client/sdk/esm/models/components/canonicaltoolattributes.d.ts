import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * The original details of a tool
 */
export type CanonicalToolAttributes = {
  /**
   * Confirmation mode for the tool
   */
  confirm?: string | undefined;
  /**
   * Prompt for the confirmation
   */
  confirmPrompt?: string | undefined;
  /**
   * Description of the tool
   */
  description: string;
  /**
   * The name of the tool
   */
  name: string;
  /**
   * Summarizer for the tool
   */
  summarizer?: string | undefined;
  /**
   * The ID of the variation that was applied to the tool
   */
  variationId: string;
};
/** @internal */
export declare const CanonicalToolAttributes$inboundSchema: z.ZodMiniType<
  CanonicalToolAttributes,
  unknown
>;
export declare function canonicalToolAttributesFromJSON(
  jsonString: string,
): SafeParseResult<CanonicalToolAttributes, SDKValidationError>;
//# sourceMappingURL=canonicaltoolattributes.d.ts.map
