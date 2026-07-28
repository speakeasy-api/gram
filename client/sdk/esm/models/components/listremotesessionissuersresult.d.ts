import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RemoteSessionIssuer } from "./remotesessionissuer.js";
/**
 * Result type for listing remote_session_issuers.
 */
export type ListRemoteSessionIssuersResult = {
  items: Array<RemoteSessionIssuer>;
  /**
   * Cursor for the next page; empty when exhausted.
   */
  nextCursor?: string | undefined;
};
/** @internal */
export declare const ListRemoteSessionIssuersResult$inboundSchema: z.ZodMiniType<
  ListRemoteSessionIssuersResult,
  unknown
>;
export declare function listRemoteSessionIssuersResultFromJSON(
  jsonString: string,
): SafeParseResult<ListRemoteSessionIssuersResult, SDKValidationError>;
//# sourceMappingURL=listremotesessionissuersresult.d.ts.map
