import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Plugin } from "./plugin.js";
export type ListPluginsResult = {
  /**
   * The plugins in the organization.
   */
  plugins: Array<Plugin>;
};
/** @internal */
export declare const ListPluginsResult$inboundSchema: z.ZodMiniType<
  ListPluginsResult,
  unknown
>;
export declare function listPluginsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListPluginsResult, SDKValidationError>;
//# sourceMappingURL=listpluginsresult.d.ts.map
