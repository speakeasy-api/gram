import * as z from "zod/v4-mini";
export type DeleteConfigRequestBody = {
    /**
     * AI provider identifier. Supported values include cursor and anthropic_compliance.
     */
    provider: string;
};
/** @internal */
export type DeleteConfigRequestBody$Outbound = {
    provider: string;
};
/** @internal */
export declare const DeleteConfigRequestBody$outboundSchema: z.ZodMiniType<DeleteConfigRequestBody$Outbound, DeleteConfigRequestBody>;
export declare function deleteConfigRequestBodyToJSON(deleteConfigRequestBody: DeleteConfigRequestBody): string;
//# sourceMappingURL=deleteconfigrequestbody.d.ts.map