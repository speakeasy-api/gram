import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { AccessMember } from "./accessmember.js";
export type ListMembersResult = {
    /**
     * The members in your organization.
     */
    members: Array<AccessMember>;
};
/** @internal */
export declare const ListMembersResult$inboundSchema: z.ZodMiniType<ListMembersResult, unknown>;
export declare function listMembersResultFromJSON(jsonString: string): SafeParseResult<ListMembersResult, SDKValidationError>;
//# sourceMappingURL=listmembersresult.d.ts.map