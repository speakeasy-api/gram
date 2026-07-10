import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListToolFiltersResult } from "../models/components/listtoolfiltersresult.js";
import { ListToolsetsResult } from "../models/components/listtoolsetsresult.js";
import { ListToolsetSummariesResult } from "../models/components/listtoolsetsummariesresult.js";
import { Toolset } from "../models/components/toolset.js";
import { AddExternalOAuthServerRequest, AddExternalOAuthServerSecurity } from "../models/operations/addexternaloauthserver.js";
import { AddOAuthProxyServerRequest, AddOAuthProxyServerSecurity } from "../models/operations/addoauthproxyserver.js";
import { CheckMCPSlugAvailabilityRequest, CheckMCPSlugAvailabilitySecurity } from "../models/operations/checkmcpslugavailability.js";
import { CloneToolsetRequest, CloneToolsetSecurity } from "../models/operations/clonetoolset.js";
import { CreateToolsetRequest, CreateToolsetSecurity } from "../models/operations/createtoolset.js";
import { DeleteToolsetRequest, DeleteToolsetSecurity } from "../models/operations/deletetoolset.js";
import { GetToolsetRequest, GetToolsetSecurity } from "../models/operations/gettoolset.js";
import { ListToolsetsRequest, ListToolsetsSecurity } from "../models/operations/listtoolsets.js";
import { ListToolsetsForOrgRequest, ListToolsetsForOrgSecurity } from "../models/operations/listtoolsetsfororg.js";
import { ListToolsetToolFiltersRequest, ListToolsetToolFiltersSecurity } from "../models/operations/listtoolsettoolfilters.js";
import { RemoveOAuthServerRequest, RemoveOAuthServerSecurity } from "../models/operations/removeoauthserver.js";
import { SetToolsetToolVariationsGroupRequest, SetToolsetToolVariationsGroupSecurity } from "../models/operations/settoolsettoolvariationsgroup.js";
import { SetToolsetUserSessionIssuerRequest, SetToolsetUserSessionIssuerSecurity } from "../models/operations/settoolsetusersessionissuer.js";
import { UpdateOAuthProxyServerRequest, UpdateOAuthProxyServerSecurity } from "../models/operations/updateoauthproxyserver.js";
import { UpdateToolsetRequest, UpdateToolsetSecurity } from "../models/operations/updatetoolset.js";
export declare class Toolsets extends ClientSDK {
    /**
     * addExternalOAuthServer toolsets
     *
     * @remarks
     * Associate an external OAuth server with a toolset
     */
    addExternalOAuthServer(request: AddExternalOAuthServerRequest, security?: AddExternalOAuthServerSecurity | undefined, options?: RequestOptions): Promise<Toolset>;
    /**
     * addOAuthProxyServer toolsets
     *
     * @remarks
     * Associate an OAuth proxy server with a toolset (admin only)
     */
    addOAuthProxyServer(request: AddOAuthProxyServerRequest, security?: AddOAuthProxyServerSecurity | undefined, options?: RequestOptions): Promise<Toolset>;
    /**
     * checkMCPSlugAvailability toolsets
     *
     * @remarks
     * Check if a MCP slug is available
     */
    checkMCPSlugAvailability(request: CheckMCPSlugAvailabilityRequest, security?: CheckMCPSlugAvailabilitySecurity | undefined, options?: RequestOptions): Promise<boolean>;
    /**
     * cloneToolset toolsets
     *
     * @remarks
     * Clone an existing toolset with a new name
     */
    cloneBySlug(request: CloneToolsetRequest, security?: CloneToolsetSecurity | undefined, options?: RequestOptions): Promise<Toolset>;
    /**
     * createToolset toolsets
     *
     * @remarks
     * Create a new toolset with associated tools
     */
    create(request: CreateToolsetRequest, security?: CreateToolsetSecurity | undefined, options?: RequestOptions): Promise<Toolset>;
    /**
     * deleteToolset toolsets
     *
     * @remarks
     * Delete a toolset by its ID
     */
    deleteBySlug(request: DeleteToolsetRequest, security?: DeleteToolsetSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * getToolset toolsets
     *
     * @remarks
     * Get detailed information about a toolset including full HTTP tool definitions
     */
    getBySlug(request: GetToolsetRequest, security?: GetToolsetSecurity | undefined, options?: RequestOptions): Promise<Toolset>;
    /**
     * listToolsets toolsets
     *
     * @remarks
     * List all toolsets for a project
     */
    list(request?: ListToolsetsRequest | undefined, security?: ListToolsetsSecurity | undefined, options?: RequestOptions): Promise<ListToolsetsResult>;
    /**
     * listToolsetsForOrg toolsets
     *
     * @remarks
     * List all toolsets across the organization (summary view)
     */
    listForOrg(request?: ListToolsetsForOrgRequest | undefined, security?: ListToolsetsForOrgSecurity | undefined, options?: RequestOptions): Promise<ListToolsetSummariesResult>;
    /**
     * listToolFilters toolsets
     *
     * @remarks
     * List the tool filter scopes (tags) available on a toolset-backed MCP server and the tools under each, including tools excluded from all filters. Read-only; reflects the explicit tool variations group configured on the toolset, deriving effective tags with the same logic as the runtime ?tags= filter. Returns filtering disabled when no explicit group is set.
     */
    listToolFilters(request: ListToolsetToolFiltersRequest, security?: ListToolsetToolFiltersSecurity | undefined, options?: RequestOptions): Promise<ListToolFiltersResult>;
    /**
     * removeOAuthServer toolsets
     *
     * @remarks
     * Remove OAuth server association from a toolset
     */
    removeOAuthServer(request: RemoveOAuthServerRequest, security?: RemoveOAuthServerSecurity | undefined, options?: RequestOptions): Promise<Toolset>;
    /**
     * setToolVariationsGroup toolsets
     *
     * @remarks
     * Assign a tool variations group to a toolset to enable MCP tool filtering (or pass null to disable). The group must already exist in the caller's project.
     */
    setToolVariationsGroup(request: SetToolsetToolVariationsGroupRequest, security?: SetToolsetToolVariationsGroupSecurity | undefined, options?: RequestOptions): Promise<Toolset>;
    /**
     * setUserSessionIssuer toolsets
     *
     * @remarks
     * Link a toolset to a user_session_issuer (or pass null to unlink). The user_session_issuer must already exist in the caller's project.
     */
    setUserSessionIssuer(request: SetToolsetUserSessionIssuerRequest, security?: SetToolsetUserSessionIssuerSecurity | undefined, options?: RequestOptions): Promise<Toolset>;
    /**
     * updateToolset toolsets
     *
     * @remarks
     * Update a toolset's properties including name, description, and HTTP tools
     */
    updateBySlug(request: UpdateToolsetRequest, security?: UpdateToolsetSecurity | undefined, options?: RequestOptions): Promise<Toolset>;
    /**
     * updateOAuthProxyServer toolsets
     *
     * @remarks
     * Update an existing OAuth proxy server associated with a toolset
     */
    updateOAuthProxyServer(request: UpdateOAuthProxyServerRequest, security?: UpdateOAuthProxyServerSecurity | undefined, options?: RequestOptions): Promise<Toolset>;
}
//# sourceMappingURL=toolsets.d.ts.map