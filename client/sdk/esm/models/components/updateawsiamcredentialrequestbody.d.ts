import * as z from "zod/v4-mini";
export type UpdateAwsIamCredentialRequestBody = {
    /**
     * The customer IAM role ARN Gram assumes. Omit for a KMS key-policy grant.
     */
    assumeRoleArn?: string | undefined;
    /**
     * The ID of the credential to update.
     */
    id: string;
    /**
     * A human-readable name for the credential.
     */
    name: string;
    /**
     * The OIDC audience. Provide (with assume_role_arn) to assume the role with a web identity.
     */
    oidcAudience?: string | undefined;
    /**
     * Optional OIDC subject pin; only valid alongside oidc_audience.
     */
    oidcSubject?: string | undefined;
    /**
     * Optional STS region override.
     */
    stsRegion?: string | undefined;
};
/** @internal */
export type UpdateAwsIamCredentialRequestBody$Outbound = {
    assume_role_arn?: string | undefined;
    id: string;
    name: string;
    oidc_audience?: string | undefined;
    oidc_subject?: string | undefined;
    sts_region?: string | undefined;
};
/** @internal */
export declare const UpdateAwsIamCredentialRequestBody$outboundSchema: z.ZodMiniType<UpdateAwsIamCredentialRequestBody$Outbound, UpdateAwsIamCredentialRequestBody>;
export declare function updateAwsIamCredentialRequestBodyToJSON(updateAwsIamCredentialRequestBody: UpdateAwsIamCredentialRequestBody): string;
//# sourceMappingURL=updateawsiamcredentialrequestbody.d.ts.map