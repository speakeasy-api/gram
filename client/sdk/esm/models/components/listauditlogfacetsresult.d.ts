import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { AuditLogFacetOption } from "./auditlogfacetoption.js";
export type ListAuditLogFacetsResult = {
  /**
   * Available action facets
   */
  actions: Array<AuditLogFacetOption>;
  /**
   * Available actor facets
   */
  actors: Array<AuditLogFacetOption>;
};
/** @internal */
export declare const ListAuditLogFacetsResult$inboundSchema: z.ZodMiniType<
  ListAuditLogFacetsResult,
  unknown
>;
export declare function listAuditLogFacetsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListAuditLogFacetsResult, SDKValidationError>;
//# sourceMappingURL=listauditlogfacetsresult.d.ts.map
