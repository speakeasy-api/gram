import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ExternalMCPServerEntry } from "./externalmcpserverentry.js";
export type ListCatalogResponseBody = {
  /**
   * Pagination cursor for the next page
   */
  nextCursor?: string | undefined;
  /**
   * List of available MCP servers
   */
  servers: Array<ExternalMCPServerEntry>;
};
/** @internal */
export declare const ListCatalogResponseBody$inboundSchema: z.ZodMiniType<
  ListCatalogResponseBody,
  unknown
>;
export declare function listCatalogResponseBodyFromJSON(
  jsonString: string,
): SafeParseResult<ListCatalogResponseBody, SDKValidationError>;
//# sourceMappingURL=listcatalogresponsebody.d.ts.map
