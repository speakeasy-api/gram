import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * The cloud provider of the credential.
 */
export declare const Provider: {
    readonly AwsIam: "aws_iam";
    readonly GcpIam: "gcp_iam";
};
/**
 * The cloud provider of the credential.
 */
export type Provider = ClosedEnum<typeof Provider>;
/**
 * An AWS IAM external credential.
 */
export type AwsIamCredential = {
    /**
     * The customer IAM role ARN Gram assumes.
     */
    assumeRoleArn?: string | undefined;
    /**
     * When the credential was created.
     */
    createdAt: Date;
    /**
     * The Gram-generated ExternalId the customer must require in their role trust policy. Present when Gram assumes the role with an ExternalId.
     */
    externalId?: string | undefined;
    /**
     * The ID of the external credential.
     */
    id: string;
    /**
     * A human-readable name for the credential.
     */
    name: string;
    /**
     * The OIDC audience. Present when Gram assumes the role with a web identity.
     */
    oidcAudience?: string | undefined;
    /**
     * Optional OIDC subject pin (web-identity approach).
     */
    oidcSubject?: string | undefined;
    /**
     * The organization that owns the credential.
     */
    organizationId: string;
    /**
     * The cloud provider of the credential.
     */
    provider: Provider;
    /**
     * Optional STS region override.
     */
    stsRegion?: string | undefined;
    /**
     * When the credential was last updated.
     */
    updatedAt: Date;
};
/** @internal */
export declare const Provider$inboundSchema: z.ZodMiniEnum<typeof Provider>;
/** @internal */
export declare const AwsIamCredential$inboundSchema: z.ZodMiniType<AwsIamCredential, unknown>;
export declare function awsIamCredentialFromJSON(jsonString: string): SafeParseResult<AwsIamCredential, SDKValidationError>;
//# sourceMappingURL=awsiamcredential.d.ts.map