import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type InstanceMcpServer = {
  /**
   * The address of the MCP server
   */
  url: string;
};
/** @internal */
export declare const InstanceMcpServer$inboundSchema: z.ZodMiniType<
  InstanceMcpServer,
  unknown
>;
export declare function instanceMcpServerFromJSON(
  jsonString: string,
): SafeParseResult<InstanceMcpServer, SDKValidationError>;
//# sourceMappingURL=instancemcpserver.d.ts.map
