import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { AuditLog } from "./auditlog.js";
export type ListAuditLogsResult = {
    /**
     * List of audit logs
     */
    logs: Array<AuditLog>;
    /**
     * The cursor to be used for the next page of results.
     */
    nextCursor?: string | undefined;
};
/** @internal */
export declare const ListAuditLogsResult$inboundSchema: z.ZodMiniType<ListAuditLogsResult, unknown>;
export declare function listAuditLogsResultFromJSON(jsonString: string): SafeParseResult<ListAuditLogsResult, SDKValidationError>;
//# sourceMappingURL=listauditlogsresult.d.ts.map