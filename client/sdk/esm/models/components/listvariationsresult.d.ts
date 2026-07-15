import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ToolVariation } from "./toolvariation.js";
export type ListVariationsResult = {
  variations: Array<ToolVariation>;
};
/** @internal */
export declare const ListVariationsResult$inboundSchema: z.ZodMiniType<
  ListVariationsResult,
  unknown
>;
export declare function listVariationsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListVariationsResult, SDKValidationError>;
//# sourceMappingURL=listvariationsresult.d.ts.map
