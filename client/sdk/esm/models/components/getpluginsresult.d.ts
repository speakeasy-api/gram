import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { AgentMarketplace } from "./agentmarketplace.js";
import { AgentPlugin } from "./agentplugin.js";
export type GetPluginsResult = {
  /**
   * Opaque revision identifier covering the marketplace + plugin set. The agent stores this to detect changes between polls.
   */
  etag: string;
  /**
   * Plugin marketplaces the agent should register with the tools it manages. Sorted by name.
   */
  marketplaces: Array<AgentMarketplace>;
  /**
   * Plugins the agent should enable. Each entry references one of the marketplaces above by name.
   */
  plugins: Array<AgentPlugin>;
};
/** @internal */
export declare const GetPluginsResult$inboundSchema: z.ZodMiniType<
  GetPluginsResult,
  unknown
>;
export declare function getPluginsResultFromJSON(
  jsonString: string,
): SafeParseResult<GetPluginsResult, SDKValidationError>;
//# sourceMappingURL=getpluginsresult.d.ts.map
