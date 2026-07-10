import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type AuditLogFacetOption = {
    /**
     * The number of audit logs for this facet value
     */
    count: number;
    /**
     * The display label shown for the facet value
     */
    displayName: string;
    /**
     * The facet value used for filtering
     */
    value: string;
};
/** @internal */
export declare const AuditLogFacetOption$inboundSchema: z.ZodMiniType<AuditLogFacetOption, unknown>;
export declare function auditLogFacetOptionFromJSON(jsonString: string): SafeParseResult<AuditLogFacetOption, SDKValidationError>;
//# sourceMappingURL=auditlogfacetoption.d.ts.map