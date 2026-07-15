import * as z from "zod/v4-mini";
export type UpdateGcpIamCredentialRequestBody = {
  /**
   * The ID of the credential to update.
   */
  id: string;
  /**
   * The service account Gram impersonates. Set alone for direct impersonation, or as the hop alongside the wif_* fields.
   */
  impersonateServiceAccount?: string | undefined;
  /**
   * A human-readable name for the credential.
   */
  name: string;
  /**
   * Workload Identity Federation pool ID. Set together with the other wif_* fields.
   */
  wifPoolId?: string | undefined;
  /**
   * GCP project number backing the WIF pool. Set together with the other wif_* fields.
   */
  wifProjectNumber?: string | undefined;
  /**
   * Workload Identity Federation provider ID. Set together with the other wif_* fields.
   */
  wifProviderId?: string | undefined;
};
/** @internal */
export type UpdateGcpIamCredentialRequestBody$Outbound = {
  id: string;
  impersonate_service_account?: string | undefined;
  name: string;
  wif_pool_id?: string | undefined;
  wif_project_number?: string | undefined;
  wif_provider_id?: string | undefined;
};
/** @internal */
export declare const UpdateGcpIamCredentialRequestBody$outboundSchema: z.ZodMiniType<
  UpdateGcpIamCredentialRequestBody$Outbound,
  UpdateGcpIamCredentialRequestBody
>;
export declare function updateGcpIamCredentialRequestBodyToJSON(
  updateGcpIamCredentialRequestBody: UpdateGcpIamCredentialRequestBody,
): string;
//# sourceMappingURL=updategcpiamcredentialrequestbody.d.ts.map
