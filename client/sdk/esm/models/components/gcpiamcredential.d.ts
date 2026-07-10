import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * The cloud provider of the credential.
 */
export declare const GcpIamCredentialProvider: {
    readonly AwsIam: "aws_iam";
    readonly GcpIam: "gcp_iam";
};
/**
 * The cloud provider of the credential.
 */
export type GcpIamCredentialProvider = ClosedEnum<typeof GcpIamCredentialProvider>;
/**
 * A GCP IAM external credential.
 */
export type GcpIamCredential = {
    /**
     * When the credential was created.
     */
    createdAt: Date;
    /**
     * The ID of the external credential.
     */
    id: string;
    /**
     * The service account Gram impersonates (impersonation approach, or the WIF hop).
     */
    impersonateServiceAccount?: string | undefined;
    /**
     * A human-readable name for the credential.
     */
    name: string;
    /**
     * The organization that owns the credential.
     */
    organizationId: string;
    /**
     * The cloud provider of the credential.
     */
    provider: GcpIamCredentialProvider;
    /**
     * When the credential was last updated.
     */
    updatedAt: Date;
    /**
     * Workload Identity Federation pool ID.
     */
    wifPoolId?: string | undefined;
    /**
     * GCP project number backing the WIF pool.
     */
    wifProjectNumber?: string | undefined;
    /**
     * Workload Identity Federation provider ID.
     */
    wifProviderId?: string | undefined;
};
/** @internal */
export declare const GcpIamCredentialProvider$inboundSchema: z.ZodMiniEnum<typeof GcpIamCredentialProvider>;
/** @internal */
export declare const GcpIamCredential$inboundSchema: z.ZodMiniType<GcpIamCredential, unknown>;
export declare function gcpIamCredentialFromJSON(jsonString: string): SafeParseResult<GcpIamCredential, SDKValidationError>;
//# sourceMappingURL=gcpiamcredential.d.ts.map