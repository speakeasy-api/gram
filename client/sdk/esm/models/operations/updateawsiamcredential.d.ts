import * as z from "zod/v4-mini";
import { UpdateAwsIamCredentialRequestBody, UpdateAwsIamCredentialRequestBody$Outbound } from "../components/updateawsiamcredentialrequestbody.js";
export type UpdateAwsIamCredentialSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type UpdateAwsIamCredentialRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    updateAwsIamCredentialRequestBody: UpdateAwsIamCredentialRequestBody;
};
/** @internal */
export type UpdateAwsIamCredentialSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdateAwsIamCredentialSecurity$outboundSchema: z.ZodMiniType<UpdateAwsIamCredentialSecurity$Outbound, UpdateAwsIamCredentialSecurity>;
export declare function updateAwsIamCredentialSecurityToJSON(updateAwsIamCredentialSecurity: UpdateAwsIamCredentialSecurity): string;
/** @internal */
export type UpdateAwsIamCredentialRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    UpdateAwsIamCredentialRequestBody: UpdateAwsIamCredentialRequestBody$Outbound;
};
/** @internal */
export declare const UpdateAwsIamCredentialRequest$outboundSchema: z.ZodMiniType<UpdateAwsIamCredentialRequest$Outbound, UpdateAwsIamCredentialRequest>;
export declare function updateAwsIamCredentialRequestToJSON(updateAwsIamCredentialRequest: UpdateAwsIamCredentialRequest): string;
//# sourceMappingURL=updateawsiamcredential.d.ts.map