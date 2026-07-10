import * as z from "zod/v4-mini";
import { RoleGrant, RoleGrant$Outbound } from "./rolegrant.js";
export type CreateRoleForm = {
    /**
     * Description of what this role can do.
     */
    description: string;
    /**
     * Scope grants to assign.
     */
    grants: Array<RoleGrant>;
    /**
     * Optional member IDs to additionally assign to this role on creation.
     */
    memberIds?: Array<string> | undefined;
    /**
     * Display name for the role.
     */
    name: string;
};
/** @internal */
export type CreateRoleForm$Outbound = {
    description: string;
    grants: Array<RoleGrant$Outbound>;
    member_ids?: Array<string> | undefined;
    name: string;
};
/** @internal */
export declare const CreateRoleForm$outboundSchema: z.ZodMiniType<CreateRoleForm$Outbound, CreateRoleForm>;
export declare function createRoleFormToJSON(createRoleForm: CreateRoleForm): string;
//# sourceMappingURL=createroleform.d.ts.map