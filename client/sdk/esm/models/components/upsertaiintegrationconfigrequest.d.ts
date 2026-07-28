import * as z from "zod/v4-mini";
export type UpsertAIIntegrationConfigRequest = {
  /**
   * Provider API key. Stored encrypted at rest; never returned on reads.
   */
  apiKey: string;
  /**
   * How the provider org is billed: 'metered', 'flat_rate', or 'unknown'. Free-form; omit to leave the existing value unchanged.
   */
  billingMode?: string | undefined;
  /**
   * Whether the integration should be active.
   */
  enabled: boolean;
  /**
   * Provider organization identifier. Required for anthropic_compliance and codex_compliance.
   */
  externalOrganizationId?: string | undefined;
  /**
   * AI provider identifier. Supported values include cursor, anthropic_compliance, and codex_compliance.
   */
  provider: string;
};
/** @internal */
export type UpsertAIIntegrationConfigRequest$Outbound = {
  api_key: string;
  billing_mode?: string | undefined;
  enabled: boolean;
  external_organization_id?: string | undefined;
  provider: string;
};
/** @internal */
export declare const UpsertAIIntegrationConfigRequest$outboundSchema: z.ZodMiniType<
  UpsertAIIntegrationConfigRequest$Outbound,
  UpsertAIIntegrationConfigRequest
>;
export declare function upsertAIIntegrationConfigRequestToJSON(
  upsertAIIntegrationConfigRequest: UpsertAIIntegrationConfigRequest,
): string;
//# sourceMappingURL=upsertaiintegrationconfigrequest.d.ts.map
