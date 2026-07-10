import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ProjectEntry } from "./projectentry.js";
export type OrganizationEntry = {
    id: string;
    name: string;
    projects: Array<ProjectEntry>;
    /**
     * Whether SCIM directory sync is enabled for this organization (synced from WorkOS)
     */
    scimEnabled?: boolean | undefined;
    slug: string;
    /**
     * Whether SSO is enabled for this organization (synced from WorkOS)
     */
    ssoEnabled?: boolean | undefined;
    userWorkspaceSlugs?: Array<string> | undefined;
};
/** @internal */
export declare const OrganizationEntry$inboundSchema: z.ZodMiniType<OrganizationEntry, unknown>;
export declare function organizationEntryFromJSON(jsonString: string): SafeParseResult<OrganizationEntry, SDKValidationError>;
//# sourceMappingURL=organizationentry.d.ts.map