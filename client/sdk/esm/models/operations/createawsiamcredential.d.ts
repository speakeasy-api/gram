import * as z from "zod/v4-mini";
import { CreateAwsIamCredentialForm, CreateAwsIamCredentialForm$Outbound } from "../components/createawsiamcredentialform.js";
export type CreateAwsIamCredentialSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type CreateAwsIamCredentialRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    createAwsIamCredentialForm: CreateAwsIamCredentialForm;
};
/** @internal */
export type CreateAwsIamCredentialSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateAwsIamCredentialSecurity$outboundSchema: z.ZodMiniType<CreateAwsIamCredentialSecurity$Outbound, CreateAwsIamCredentialSecurity>;
export declare function createAwsIamCredentialSecurityToJSON(createAwsIamCredentialSecurity: CreateAwsIamCredentialSecurity): string;
/** @internal */
export type CreateAwsIamCredentialRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    CreateAwsIamCredentialForm: CreateAwsIamCredentialForm$Outbound;
};
/** @internal */
export declare const CreateAwsIamCredentialRequest$outboundSchema: z.ZodMiniType<CreateAwsIamCredentialRequest$Outbound, CreateAwsIamCredentialRequest>;
export declare function createAwsIamCredentialRequestToJSON(createAwsIamCredentialRequest: CreateAwsIamCredentialRequest): string;
//# sourceMappingURL=createawsiamcredential.d.ts.map