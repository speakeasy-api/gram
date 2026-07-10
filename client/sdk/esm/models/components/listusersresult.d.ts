import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { OrganizationUser } from "./organizationuser.js";
export type ListUsersResult = {
    /**
     * Users linked to the organization in Gram.
     */
    users: Array<OrganizationUser>;
};
/** @internal */
export declare const ListUsersResult$inboundSchema: z.ZodMiniType<ListUsersResult, unknown>;
export declare function listUsersResultFromJSON(jsonString: string): SafeParseResult<ListUsersResult, SDKValidationError>;
//# sourceMappingURL=listusersresult.d.ts.map