import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { BuiltinExclusionCategory } from "./builtinexclusioncategory.js";
export type ListBuiltinExclusionsResult = {
  /**
   * The library grouped by category.
   */
  categories: Array<BuiltinExclusionCategory>;
  /**
   * Catalog checksum/version, for provenance.
   */
  version: string;
};
/** @internal */
export declare const ListBuiltinExclusionsResult$inboundSchema: z.ZodMiniType<
  ListBuiltinExclusionsResult,
  unknown
>;
export declare function listBuiltinExclusionsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListBuiltinExclusionsResult, SDKValidationError>;
//# sourceMappingURL=listbuiltinexclusionsresult.d.ts.map
