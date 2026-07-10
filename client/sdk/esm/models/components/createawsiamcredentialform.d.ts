import * as z from "zod/v4-mini";
export type CreateAwsIamCredentialForm = {
    /**
     * The customer IAM role ARN Gram assumes. Omit for a KMS key-policy grant.
     */
    assumeRoleArn?: string | undefined;
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
export type CreateAwsIamCredentialForm$Outbound = {
    assume_role_arn?: string | undefined;
    name: string;
    oidc_audience?: string | undefined;
    oidc_subject?: string | undefined;
    sts_region?: string | undefined;
};
/** @internal */
export declare const CreateAwsIamCredentialForm$outboundSchema: z.ZodMiniType<CreateAwsIamCredentialForm$Outbound, CreateAwsIamCredentialForm>;
export declare function createAwsIamCredentialFormToJSON(createAwsIamCredentialForm: CreateAwsIamCredentialForm): string;
//# sourceMappingURL=createawsiamcredentialform.d.ts.map