import * as z from "zod/v4-mini";
export type DeleteRequestBody = {
  /**
   * Override ID to delete
   */
  overrideId: string;
};
/** @internal */
export type DeleteRequestBody$Outbound = {
  override_id: string;
};
/** @internal */
export declare const DeleteRequestBody$outboundSchema: z.ZodMiniType<
  DeleteRequestBody$Outbound,
  DeleteRequestBody
>;
export declare function deleteRequestBodyToJSON(
  deleteRequestBody: DeleteRequestBody,
): string;
//# sourceMappingURL=deleterequestbody.d.ts.map
