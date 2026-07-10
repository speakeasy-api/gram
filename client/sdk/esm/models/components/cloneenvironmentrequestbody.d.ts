import * as z from "zod/v4-mini";
export type CloneEnvironmentRequestBody = {
    /**
     * If true, copy the encrypted secret values from the source. If false (default), copy only variable names with empty placeholder values.
     */
    copyValues?: boolean | undefined;
    /**
     * The name for the new cloned environment
     */
    newName: string;
};
/** @internal */
export type CloneEnvironmentRequestBody$Outbound = {
    copy_values?: boolean | undefined;
    new_name: string;
};
/** @internal */
export declare const CloneEnvironmentRequestBody$outboundSchema: z.ZodMiniType<CloneEnvironmentRequestBody$Outbound, CloneEnvironmentRequestBody>;
export declare function cloneEnvironmentRequestBodyToJSON(cloneEnvironmentRequestBody: CloneEnvironmentRequestBody): string;
//# sourceMappingURL=cloneenvironmentrequestbody.d.ts.map