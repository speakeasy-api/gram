import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * HTTP header forwarded with each OTEL payload.
 */
export type OtelForwardingHeader = {
    /**
     * Whether a non-empty value is currently stored for this header. Always false on write-only operations.
     */
    hasValue: boolean;
    /**
     * Header name.
     */
    name: string;
};
/** @internal */
export declare const OtelForwardingHeader$inboundSchema: z.ZodMiniType<OtelForwardingHeader, unknown>;
export declare function otelForwardingHeaderFromJSON(jsonString: string): SafeParseResult<OtelForwardingHeader, SDKValidationError>;
//# sourceMappingURL=otelforwardingheader.d.ts.map