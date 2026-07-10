import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Derived status for the latest usage poll state. Omitted when no config is set for the provider.
 */
export declare const LastPollStatus: {
    readonly Pending: "pending";
    readonly Success: "success";
    readonly Failed: "failed";
};
/**
 * Derived status for the latest usage poll state. Omitted when no config is set for the provider.
 */
export type LastPollStatus = ClosedEnum<typeof LastPollStatus>;
/**
 * Per-organization AI provider integration config. The provider API key is write-only; reads only expose whether a key is configured.
 */
export type AIIntegrationConfig = {
    /**
     * How the provider org is billed: 'metered' (pay-per-token; dashboard cost is real spend), 'flat_rate' (subscription seats; cost is an estimate), or 'unknown'. Empty/omitted when not declared.
     */
    billingMode?: string | undefined;
    /**
     * ISO 8601 timestamp when the config was created. Omitted when no config is set.
     */
    createdAt?: Date | undefined;
    /**
     * Whether the provider integration is active.
     */
    enabled: boolean;
    /**
     * Provider organization identifier. Required for anthropic_compliance; omitted for providers that do not need one.
     */
    externalOrganizationId?: string | undefined;
    /**
     * Whether an API key is currently stored. The key itself is never returned.
     */
    hasApiKey: boolean;
    /**
     * Config ID. Omitted when no config is set for the provider.
     */
    id?: string | undefined;
    /**
     * Stored error from the latest failed usage poll. Omitted unless the latest poll state failed.
     */
    lastPollError?: string | undefined;
    /**
     * ISO 8601 timestamp for the latest failed usage poll. Omitted unless a poll has failed.
     */
    lastPollFailedAt?: Date | undefined;
    /**
     * Derived status for the latest usage poll state. Omitted when no config is set for the provider.
     */
    lastPollStatus?: LastPollStatus | undefined;
    /**
     * ISO 8601 timestamp for the last successful usage poll. Omitted until a poll succeeds.
     */
    lastPolledAt?: Date | undefined;
    /**
     * ISO 8601 timestamp for the next scheduled usage poll. Omitted when no config is set.
     */
    nextPollAfter?: Date | undefined;
    /**
     * Organization the config belongs to.
     */
    organizationId: string;
    /**
     * Project used as the telemetry write target. Omitted when no config is set.
     */
    projectId?: string | undefined;
    /**
     * AI provider identifier. Supported values include cursor and anthropic_compliance.
     */
    provider: string;
    /**
     * ISO 8601 timestamp of the most recent change. Omitted when no config is set.
     */
    updatedAt?: Date | undefined;
};
/** @internal */
export declare const LastPollStatus$inboundSchema: z.ZodMiniEnum<typeof LastPollStatus>;
/** @internal */
export declare const AIIntegrationConfig$inboundSchema: z.ZodMiniType<AIIntegrationConfig, unknown>;
export declare function aiIntegrationConfigFromJSON(jsonString: string): SafeParseResult<AIIntegrationConfig, SDKValidationError>;
//# sourceMappingURL=aiintegrationconfig.d.ts.map