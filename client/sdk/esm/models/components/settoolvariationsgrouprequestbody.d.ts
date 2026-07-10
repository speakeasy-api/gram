import * as z from "zod/v4-mini";
export type SetToolVariationsGroupRequestBody = {
    /**
     * The tool variations group id to assign, or null to disable filtering.
     */
    toolVariationsGroupId?: string | undefined;
};
/** @internal */
export type SetToolVariationsGroupRequestBody$Outbound = {
    tool_variations_group_id?: string | undefined;
};
/** @internal */
export declare const SetToolVariationsGroupRequestBody$outboundSchema: z.ZodMiniType<SetToolVariationsGroupRequestBody$Outbound, SetToolVariationsGroupRequestBody>;
export declare function setToolVariationsGroupRequestBodyToJSON(setToolVariationsGroupRequestBody: SetToolVariationsGroupRequestBody): string;
//# sourceMappingURL=settoolvariationsgrouprequestbody.d.ts.map