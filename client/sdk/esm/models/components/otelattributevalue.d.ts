import * as z from "zod/v4-mini";
/**
 * OTEL attribute value - any of the OTLP/JSON value kinds
 */
export type OTELAttributeValue = {
    /**
     * Array value (passed through)
     */
    arrayValue?: any | undefined;
    /**
     * Boolean value
     */
    boolValue?: boolean | undefined;
    /**
     * Bytes value (base64-encoded per OTLP/JSON)
     */
    bytesValue?: string | undefined;
    /**
     * Double value
     */
    doubleValue?: number | undefined;
    /**
     * Integer value (string-encoded per OTLP/JSON, or raw number)
     */
    intValue?: any | undefined;
    /**
     * Key-value list value (passed through)
     */
    kvlistValue?: any | undefined;
    /**
     * String value
     */
    stringValue?: string | undefined;
};
/** @internal */
export type OTELAttributeValue$Outbound = {
    arrayValue?: any | undefined;
    boolValue?: boolean | undefined;
    bytesValue?: string | undefined;
    doubleValue?: number | undefined;
    intValue?: any | undefined;
    kvlistValue?: any | undefined;
    stringValue?: string | undefined;
};
/** @internal */
export declare const OTELAttributeValue$outboundSchema: z.ZodMiniType<OTELAttributeValue$Outbound, OTELAttributeValue>;
export declare function otelAttributeValueToJSON(otelAttributeValue: OTELAttributeValue): string;
//# sourceMappingURL=otelattributevalue.d.ts.map