import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { Environment } from "../models/components/environment.js";
import { ListEnvironmentsResult } from "../models/components/listenvironmentsresult.js";
import { SourceEnvironmentLink } from "../models/components/sourceenvironmentlink.js";
import { ToolsetEnvironmentLink } from "../models/components/toolsetenvironmentlink.js";
import { CloneEnvironmentRequest, CloneEnvironmentSecurity } from "../models/operations/cloneenvironment.js";
import { CreateEnvironmentRequest, CreateEnvironmentSecurity } from "../models/operations/createenvironment.js";
import { DeleteEnvironmentRequest, DeleteEnvironmentSecurity } from "../models/operations/deleteenvironment.js";
import { DeleteSourceEnvironmentLinkRequest, DeleteSourceEnvironmentLinkSecurity } from "../models/operations/deletesourceenvironmentlink.js";
import { DeleteToolsetEnvironmentLinkRequest, DeleteToolsetEnvironmentLinkSecurity } from "../models/operations/deletetoolsetenvironmentlink.js";
import { GetSourceEnvironmentRequest, GetSourceEnvironmentSecurity } from "../models/operations/getsourceenvironment.js";
import { GetToolsetEnvironmentRequest, GetToolsetEnvironmentSecurity } from "../models/operations/gettoolsetenvironment.js";
import { ListEnvironmentsRequest, ListEnvironmentsSecurity } from "../models/operations/listenvironments.js";
import { SetSourceEnvironmentLinkRequest, SetSourceEnvironmentLinkSecurity } from "../models/operations/setsourceenvironmentlink.js";
import { SetToolsetEnvironmentLinkRequest, SetToolsetEnvironmentLinkSecurity } from "../models/operations/settoolsetenvironmentlink.js";
import { UpdateEnvironmentRequest, UpdateEnvironmentSecurity } from "../models/operations/updateenvironment.js";
export declare class Environments extends ClientSDK {
    /**
     * cloneEnvironment environments
     *
     * @remarks
     * Clone an environment into a new one. Either copies only the variable names with empty placeholder values, or copies the encrypted values verbatim. Encrypted secret values are never decrypted by the application during the clone operation.
     */
    clone(request: CloneEnvironmentRequest, security?: CloneEnvironmentSecurity | undefined, options?: RequestOptions): Promise<Environment>;
    /**
     * createEnvironment environments
     *
     * @remarks
     * Create a new environment
     */
    create(request: CreateEnvironmentRequest, security?: CreateEnvironmentSecurity | undefined, options?: RequestOptions): Promise<Environment>;
    /**
     * deleteEnvironment environments
     *
     * @remarks
     * Delete an environment
     */
    deleteBySlug(request: DeleteEnvironmentRequest, security?: DeleteEnvironmentSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * deleteSourceEnvironmentLink environments
     *
     * @remarks
     * Delete a link between a source and an environment
     */
    deleteSourceLink(request: DeleteSourceEnvironmentLinkRequest, security?: DeleteSourceEnvironmentLinkSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * deleteToolsetEnvironmentLink environments
     *
     * @remarks
     * Delete a link between a toolset and an environment
     */
    deleteToolsetLink(request: DeleteToolsetEnvironmentLinkRequest, security?: DeleteToolsetEnvironmentLinkSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * getSourceEnvironment environments
     *
     * @remarks
     * Get the environment linked to a source
     */
    getBySource(request: GetSourceEnvironmentRequest, security?: GetSourceEnvironmentSecurity | undefined, options?: RequestOptions): Promise<Environment>;
    /**
     * getToolsetEnvironment environments
     *
     * @remarks
     * Get the environment linked to a toolset
     */
    getByToolset(request: GetToolsetEnvironmentRequest, security?: GetToolsetEnvironmentSecurity | undefined, options?: RequestOptions): Promise<Environment>;
    /**
     * listEnvironments environments
     *
     * @remarks
     * List all environments for an organization
     */
    list(request?: ListEnvironmentsRequest | undefined, security?: ListEnvironmentsSecurity | undefined, options?: RequestOptions): Promise<ListEnvironmentsResult>;
    /**
     * setSourceEnvironmentLink environments
     *
     * @remarks
     * Set (upsert) a link between a source and an environment
     */
    setSourceLink(request: SetSourceEnvironmentLinkRequest, security?: SetSourceEnvironmentLinkSecurity | undefined, options?: RequestOptions): Promise<SourceEnvironmentLink>;
    /**
     * setToolsetEnvironmentLink environments
     *
     * @remarks
     * Set (upsert) a link between a toolset and an environment
     */
    setToolsetLink(request: SetToolsetEnvironmentLinkRequest, security?: SetToolsetEnvironmentLinkSecurity | undefined, options?: RequestOptions): Promise<ToolsetEnvironmentLink>;
    /**
     * updateEnvironment environments
     *
     * @remarks
     * Update an environment
     */
    updateBySlug(request: UpdateEnvironmentRequest, security?: UpdateEnvironmentSecurity | undefined, options?: RequestOptions): Promise<Environment>;
}
//# sourceMappingURL=environments.d.ts.map