import * as z from "zod/v4-mini";
/**
 * An org-scoped time window, optionally narrowed to one project
 */
export type TelemetryWindowPayload = {
    /**
     * Start time in ISO 8601 format
     */
    from: Date;
    /**
     * Optional project to scope to; defaults to every project in the organization.
     */
    projectId?: string | undefined;
    /**
     * End time in ISO 8601 format
     */
    to: Date;
};
/** @internal */
export type TelemetryWindowPayload$Outbound = {
    from: string;
    project_id?: string | undefined;
    to: string;
};
/** @internal */
export declare const TelemetryWindowPayload$outboundSchema: z.ZodMiniType<TelemetryWindowPayload$Outbound, TelemetryWindowPayload>;
export declare function telemetryWindowPayloadToJSON(telemetryWindowPayload: TelemetryWindowPayload): string;
//# sourceMappingURL=telemetrywindowpayload.d.ts.map