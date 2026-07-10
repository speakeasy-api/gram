import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Tool } from "./tool.js";
export type ListToolsResult = {
  /**
   * The cursor to fetch results from
   */
  nextCursor?: string | undefined;
  /**
   * The list of tools (polymorphic union of HTTP tools and prompt templates)
   */
  tools: Array<Tool>;
};
/** @internal */
export declare const ListToolsResult$inboundSchema: z.ZodMiniType<
  ListToolsResult,
  unknown
>;
export declare function listToolsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListToolsResult, SDKValidationError>;
//# sourceMappingURL=listtoolsresult.d.ts.map
