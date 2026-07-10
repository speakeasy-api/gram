import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { BuiltinExclusionEntry } from "./builtinexclusionentry.js";
/**
 * A named group of built-in exclusion rules.
 */
export type BuiltinExclusionCategory = {
  /**
   * The rules in this category.
   */
  entries: Array<BuiltinExclusionEntry>;
  /**
   * Human category label, e.g. "Test credit cards".
   */
  label: string;
};
/** @internal */
export declare const BuiltinExclusionCategory$inboundSchema: z.ZodMiniType<
  BuiltinExclusionCategory,
  unknown
>;
export declare function builtinExclusionCategoryFromJSON(
  jsonString: string,
): SafeParseResult<BuiltinExclusionCategory, SDKValidationError>;
//# sourceMappingURL=builtinexclusioncategory.d.ts.map
