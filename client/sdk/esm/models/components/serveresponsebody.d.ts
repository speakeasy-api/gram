import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ExternalMCPServer } from "./externalmcpserver.js";
export type ServeResponseBody = {
  /**
   * Pagination cursor for the next page
   */
  nextCursor?: string | undefined;
  /**
   * List of available MCP servers
   */
  servers: Array<ExternalMCPServer>;
};
/** @internal */
export declare const ServeResponseBody$inboundSchema: z.ZodMiniType<
  ServeResponseBody,
  unknown
>;
export declare function serveResponseBodyFromJSON(
  jsonString: string,
): SafeParseResult<ServeResponseBody, SDKValidationError>;
//# sourceMappingURL=serveresponsebody.d.ts.map
