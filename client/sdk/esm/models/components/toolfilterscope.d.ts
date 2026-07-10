import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ToolFilterTool } from "./toolfiltertool.js";
/**
 * A filter tag ("scope") and the tools reachable when filtering by it via the runtime ?tags= parameter.
 */
export type ToolFilterScope = {
  /**
   * The filter tag
   */
  tag: string;
  /**
   * The number of tools under this scope
   */
  toolCount: number;
  /**
   * The tools under this scope
   */
  tools: Array<ToolFilterTool>;
};
/** @internal */
export declare const ToolFilterScope$inboundSchema: z.ZodMiniType<
  ToolFilterScope,
  unknown
>;
export declare function toolFilterScopeFromJSON(
  jsonString: string,
): SafeParseResult<ToolFilterScope, SDKValidationError>;
//# sourceMappingURL=toolfilterscope.d.ts.map
