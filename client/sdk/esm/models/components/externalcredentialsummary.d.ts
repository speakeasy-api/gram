import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * The cloud provider of the credential.
 */
export declare const ExternalCredentialSummaryProvider: {
  readonly AwsIam: "aws_iam";
  readonly GcpIam: "gcp_iam";
};
/**
 * The cloud provider of the credential.
 */
export type ExternalCredentialSummaryProvider = ClosedEnum<
  typeof ExternalCredentialSummaryProvider
>;
/**
 * Provider-independent summary of an external credential.
 */
export type ExternalCredentialSummary = {
  /**
   * When the credential was created.
   */
  createdAt: Date;
  /**
   * The ID of the external credential.
   */
  id: string;
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
  provider: ExternalCredentialSummaryProvider;
  /**
   * When the credential was last updated.
   */
  updatedAt: Date;
};
/** @internal */
export declare const ExternalCredentialSummaryProvider$inboundSchema: z.ZodMiniEnum<
  typeof ExternalCredentialSummaryProvider
>;
/** @internal */
export declare const ExternalCredentialSummary$inboundSchema: z.ZodMiniType<
  ExternalCredentialSummary,
  unknown
>;
export declare function externalCredentialSummaryFromJSON(
  jsonString: string,
): SafeParseResult<ExternalCredentialSummary, SDKValidationError>;
//# sourceMappingURL=externalcredentialsummary.d.ts.map
