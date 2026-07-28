import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RemoteSessionClient } from "./remotesessionclient.js";
/**
 * Result type for listing remote_session_clients.
 */
export type ListRemoteSessionClientsResult = {
  items: Array<RemoteSessionClient>;
  /**
   * Cursor for the next page; empty when exhausted.
   */
  nextCursor?: string | undefined;
};
/** @internal */
export declare const ListRemoteSessionClientsResult$inboundSchema: z.ZodMiniType<
  ListRemoteSessionClientsResult,
  unknown
>;
export declare function listRemoteSessionClientsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListRemoteSessionClientsResult, SDKValidationError>;
//# sourceMappingURL=listremotesessionclientsresult.d.ts.map
