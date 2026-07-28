import * as z from "zod/v4-mini";
export type UpsertRequestBody = {
  /**
   * User-friendly display name
   */
  displayName: string;
  /**
   * Original server name from hooks
   */
  rawServerName: string;
};
/** @internal */
export type UpsertRequestBody$Outbound = {
  display_name: string;
  raw_server_name: string;
};
/** @internal */
export declare const UpsertRequestBody$outboundSchema: z.ZodMiniType<
  UpsertRequestBody$Outbound,
  UpsertRequestBody
>;
export declare function upsertRequestBodyToJSON(
  upsertRequestBody: UpsertRequestBody,
): string;
//# sourceMappingURL=upsertrequestbody.d.ts.map
