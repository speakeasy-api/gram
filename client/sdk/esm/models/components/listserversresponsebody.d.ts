import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ExternalMCPServer } from "./externalmcpserver.js";
export type ListServersResponseBody = {
  /**
   * List of available MCP servers
   */
  servers: Array<ExternalMCPServer>;
};
/** @internal */
export declare const ListServersResponseBody$inboundSchema: z.ZodMiniType<
  ListServersResponseBody,
  unknown
>;
export declare function listServersResponseBodyFromJSON(
  jsonString: string,
): SafeParseResult<ListServersResponseBody, SDKValidationError>;
//# sourceMappingURL=listserversresponsebody.d.ts.map
