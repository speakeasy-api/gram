import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ToolFilterScope } from "./toolfilterscope.js";
import { ToolFilterTool } from "./toolfiltertool.js";
/**
 * The tool filtering configuration in effect for an MCP server: the resolved tool variations group, the available filter scopes (tags), and the tools excluded from all filters. Read-only. Filtering is reported as enabled only when an explicit tool variations group is configured on the MCP server or its toolset; the per-group effective-tag derivation matches the runtime ?tags= filter for that group.
 */
export type ListToolFiltersResult = {
  /**
   * Tools whose effective tag set is empty: reachable only without a ?tags= filter.
   */
  excluded: Array<ToolFilterTool>;
  /**
   * Whether tool filtering is enabled, i.e. the resolution chain (mcp_servers then toolsets) yields a non-null tool variations group. When false, scopes and excluded are empty. A project-default (source-level) variations group is not treated as filtering here.
   */
  filteringEnabled: boolean;
  /**
   * The available filter scopes (tags), each with its member tools. Union of effective tags across the server's tools.
   */
  scopes: Array<ToolFilterScope>;
  /**
   * The ID of the resolved tool variations group, if filtering is enabled.
   */
  toolVariationsGroupId?: string | undefined;
  /**
   * The name of the resolved tool variations group, if filtering is enabled.
   */
  toolVariationsGroupName?: string | undefined;
};
/** @internal */
export declare const ListToolFiltersResult$inboundSchema: z.ZodMiniType<
  ListToolFiltersResult,
  unknown
>;
export declare function listToolFiltersResultFromJSON(
  jsonString: string,
): SafeParseResult<ListToolFiltersResult, SDKValidationError>;
//# sourceMappingURL=listtoolfiltersresult.d.ts.map
