import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export type ListToolsSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
/**
 * The type of tool
 */
export declare const ToolTypes: {
  readonly Http: "http";
  readonly Prompt: "prompt";
  readonly Function: "function";
  readonly Platform: "platform";
  readonly Externalmcp: "externalmcp";
};
/**
 * The type of tool
 */
export type ToolTypes = ClosedEnum<typeof ToolTypes>;
export type ListToolsRequest = {
  /**
   * The cursor to fetch results from
   */
  cursor?: string | undefined;
  /**
   * The number of tools to return per page
   */
  limit?: number | undefined;
  /**
   * The deployment ID. If unset, latest deployment will be used.
   */
  deploymentId?: string | undefined;
  /**
   * Filter tools by URN prefix (e.g. 'tools:http:kitchen-sink' to match all tools starting with that prefix)
   */
  urnPrefix?: string | undefined;
  toolTypes?: Array<ToolTypes> | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
};
/** @internal */
export type ListToolsSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListToolsSecurity$outboundSchema: z.ZodMiniType<
  ListToolsSecurity$Outbound,
  ListToolsSecurity
>;
export declare function listToolsSecurityToJSON(
  listToolsSecurity: ListToolsSecurity,
): string;
/** @internal */
export declare const ToolTypes$outboundSchema: z.ZodMiniEnum<typeof ToolTypes>;
/** @internal */
export type ListToolsRequest$Outbound = {
  cursor?: string | undefined;
  limit?: number | undefined;
  deployment_id?: string | undefined;
  urn_prefix?: string | undefined;
  tool_types?: Array<string> | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListToolsRequest$outboundSchema: z.ZodMiniType<
  ListToolsRequest$Outbound,
  ListToolsRequest
>;
export declare function listToolsRequestToJSON(
  listToolsRequest: ListToolsRequest,
): string;
//# sourceMappingURL=listtools.d.ts.map
