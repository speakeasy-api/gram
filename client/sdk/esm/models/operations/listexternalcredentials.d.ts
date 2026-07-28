import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export type ListExternalCredentialsSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
/**
 * Only return credentials for this provider.
 */
export declare const Provider: {
  readonly AwsIam: "aws_iam";
  readonly GcpIam: "gcp_iam";
};
/**
 * Only return credentials for this provider.
 */
export type Provider = ClosedEnum<typeof Provider>;
export type ListExternalCredentialsRequest = {
  /**
   * Only return credentials for this provider.
   */
  provider?: Provider | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type ListExternalCredentialsSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListExternalCredentialsSecurity$outboundSchema: z.ZodMiniType<
  ListExternalCredentialsSecurity$Outbound,
  ListExternalCredentialsSecurity
>;
export declare function listExternalCredentialsSecurityToJSON(
  listExternalCredentialsSecurity: ListExternalCredentialsSecurity,
): string;
/** @internal */
export declare const Provider$outboundSchema: z.ZodMiniEnum<typeof Provider>;
/** @internal */
export type ListExternalCredentialsRequest$Outbound = {
  provider?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListExternalCredentialsRequest$outboundSchema: z.ZodMiniType<
  ListExternalCredentialsRequest$Outbound,
  ListExternalCredentialsRequest
>;
export declare function listExternalCredentialsRequestToJSON(
  listExternalCredentialsRequest: ListExternalCredentialsRequest,
): string;
//# sourceMappingURL=listexternalcredentials.d.ts.map
