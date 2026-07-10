import * as z from "zod/v4-mini";
export type GetAwsIamCredentialSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type GetAwsIamCredentialRequest = {
    /**
     * The ID of the credential to get.
     */
    id: string;
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type GetAwsIamCredentialSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetAwsIamCredentialSecurity$outboundSchema: z.ZodMiniType<GetAwsIamCredentialSecurity$Outbound, GetAwsIamCredentialSecurity>;
export declare function getAwsIamCredentialSecurityToJSON(getAwsIamCredentialSecurity: GetAwsIamCredentialSecurity): string;
/** @internal */
export type GetAwsIamCredentialRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetAwsIamCredentialRequest$outboundSchema: z.ZodMiniType<GetAwsIamCredentialRequest$Outbound, GetAwsIamCredentialRequest>;
export declare function getAwsIamCredentialRequestToJSON(getAwsIamCredentialRequest: GetAwsIamCredentialRequest): string;
//# sourceMappingURL=getawsiamcredential.d.ts.map