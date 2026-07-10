import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { OtelForwardingHeader } from "./otelforwardingheader.js";
/**
 * Per-organization config that controls forwarding of OTEL payloads received on the hooks endpoints to a customer-owned URL. When no config is set, id/created_at/updated_at are omitted and enabled defaults to false.
 */
export type OtelForwardingConfig = {
    /**
     * ISO 8601 timestamp when the config was created. Omitted when no config is set.
     */
    createdAt?: Date | undefined;
    /**
     * Whether forwarding is currently active.
     */
    enabled: boolean;
    /**
     * URL each OTEL payload is POSTed to. Empty string when no config is set.
     */
    endpointUrl: string;
    /**
     * Headers configured for this endpoint. Values are never returned.
     */
    headers: Array<OtelForwardingHeader>;
    /**
     * Config ID. Omitted when no config is set for the organization.
     */
    id?: string | undefined;
    /**
     * Organization the config belongs to.
     */
    organizationId: string;
    /**
     * ISO 8601 timestamp of the most recent change. Omitted when no config is set.
     */
    updatedAt?: Date | undefined;
};
/** @internal */
export declare const OtelForwardingConfig$inboundSchema: z.ZodMiniType<OtelForwardingConfig, unknown>;
export declare function otelForwardingConfigFromJSON(jsonString: string): SafeParseResult<OtelForwardingConfig, SDKValidationError>;
//# sourceMappingURL=otelforwardingconfig.d.ts.map