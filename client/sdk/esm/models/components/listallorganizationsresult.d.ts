import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { OrganizationSummary } from "./organizationsummary.js";
export type ListAllOrganizationsResult = {
    /**
     * Maximum number of organizations returned in this response.
     */
    limit: number;
    /**
     * Number of organizations skipped before this page.
     */
    offset: number;
    /**
     * Gram organizations for this page.
     */
    organizations: Array<OrganizationSummary>;
    /**
     * Total number of Gram organizations (ignores limit/offset).
     */
    total: number;
};
/** @internal */
export declare const ListAllOrganizationsResult$inboundSchema: z.ZodMiniType<ListAllOrganizationsResult, unknown>;
export declare function listAllOrganizationsResultFromJSON(jsonString: string): SafeParseResult<ListAllOrganizationsResult, SDKValidationError>;
//# sourceMappingURL=listallorganizationsresult.d.ts.map