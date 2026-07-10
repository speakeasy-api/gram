import * as z from "zod/v4-mini";
export type DeleteCustomDetectionRuleRequestBody = {
    /**
     * The custom detection rule ID.
     */
    id: string;
};
/** @internal */
export type DeleteCustomDetectionRuleRequestBody$Outbound = {
    id: string;
};
/** @internal */
export declare const DeleteCustomDetectionRuleRequestBody$outboundSchema: z.ZodMiniType<DeleteCustomDetectionRuleRequestBody$Outbound, DeleteCustomDetectionRuleRequestBody>;
export declare function deleteCustomDetectionRuleRequestBodyToJSON(deleteCustomDetectionRuleRequestBody: DeleteCustomDetectionRuleRequestBody): string;
//# sourceMappingURL=deletecustomdetectionrulerequestbody.d.ts.map