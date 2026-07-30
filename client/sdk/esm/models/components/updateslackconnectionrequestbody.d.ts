import * as z from "zod/v3";
export type UpdateSlackConnectionRequestBody = {
  /**
   * The default toolset slug for this Slack connection
   */
  defaultToolsetSlug: string;
};
/** @internal */
export type UpdateSlackConnectionRequestBody$Outbound = {
  default_toolset_slug: string;
};
/** @internal */
export declare const UpdateSlackConnectionRequestBody$outboundSchema: z.ZodType<
  UpdateSlackConnectionRequestBody$Outbound,
  z.ZodTypeDef,
  UpdateSlackConnectionRequestBody
>;
export declare function updateSlackConnectionRequestBodyToJSON(
  updateSlackConnectionRequestBody: UpdateSlackConnectionRequestBody,
): string;
//# sourceMappingURL=updateslackconnectionrequestbody.d.ts.map
