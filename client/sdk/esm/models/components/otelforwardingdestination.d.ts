import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { OtelForwardingHeader } from "./otelforwardingheader.js";
/**
 * A configured OTEL forwarding endpoint owned by an organization (optionally scoped to a project).
 */
export type OtelForwardingDestination = {
    /**
     * ISO 8601 timestamp when the destination was created.
     */
    createdAt: Date;
    /**
     * Whether forwarding to this destination is currently active.
     */
    enabled: boolean;
    /**
     * URL each OTEL payload is POSTed to.
     */
    endpointUrl: string;
    /**
     * Headers configured for this destination. Values are never returned.
     */
    headers: Array<OtelForwardingHeader>;
    /**
     * Destination ID.
     */
    id: string;
    /**
     * Human-readable name. Unique within (org, project).
     */
    name: string;
    /**
     * Organization the destination belongs to.
     */
    organizationId: string;
    /**
     * Project the destination belongs to. Omitted for org-wide destinations.
     */
    projectId?: string | undefined;
    /**
     * ISO 8601 timestamp of the most recent change.
     */
    updatedAt: Date;
};
/** @internal */
export declare const OtelForwardingDestination$inboundSchema: z.ZodMiniType<OtelForwardingDestination, unknown>;
export declare function otelForwardingDestinationFromJSON(jsonString: string): SafeParseResult<OtelForwardingDestination, SDKValidationError>;
//# sourceMappingURL=otelforwardingdestination.d.ts.map