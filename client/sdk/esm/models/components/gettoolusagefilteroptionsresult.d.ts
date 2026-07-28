import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ToolUsageHostedServerFilterOption } from "./toolusagehostedserverfilteroption.js";
import { ToolUsageShadowServerFilterOption } from "./toolusageshadowserverfilteroption.js";
import { ToolUsageUserFilterOption } from "./toolusageuserfilteroption.js";
/**
 * Filter options for target-aware MCP and tool usage metrics
 */
export type GetToolUsageFilterOptionsResult = {
  /**
   * Hosted MCP servers with usage in the selected time range
   */
  hostedServers: Array<ToolUsageHostedServerFilterOption>;
  /**
   * Shadow MCP servers with usage in the selected time range
   */
  shadowServers: Array<ToolUsageShadowServerFilterOption>;
  /**
   * User identities with usage in the selected time range
   */
  users: Array<ToolUsageUserFilterOption>;
};
/** @internal */
export declare const GetToolUsageFilterOptionsResult$inboundSchema: z.ZodMiniType<
  GetToolUsageFilterOptionsResult,
  unknown
>;
export declare function getToolUsageFilterOptionsResultFromJSON(
  jsonString: string,
): SafeParseResult<GetToolUsageFilterOptionsResult, SDKValidationError>;
//# sourceMappingURL=gettoolusagefilteroptionsresult.d.ts.map
