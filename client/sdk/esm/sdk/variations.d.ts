import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { DeleteGlobalToolVariationResult } from "../models/components/deleteglobaltoolvariationresult.js";
import { ListToolVariationGroupsResult } from "../models/components/listtoolvariationgroupsresult.js";
import { ListVariationsResult } from "../models/components/listvariationsresult.js";
import { ToolVariationGroupResult } from "../models/components/toolvariationgroupresult.js";
import { UpsertGlobalToolVariationResult } from "../models/components/upsertglobaltoolvariationresult.js";
import { CreateGlobalToolVariationGroupRequest, CreateGlobalToolVariationGroupSecurity } from "../models/operations/createglobaltoolvariationgroup.js";
import { DeleteGlobalVariationRequest, DeleteGlobalVariationSecurity } from "../models/operations/deleteglobalvariation.js";
import { ListGlobalVariationsRequest, ListGlobalVariationsSecurity } from "../models/operations/listglobalvariations.js";
import { ListToolVariationGroupsRequest, ListToolVariationGroupsSecurity } from "../models/operations/listtoolvariationgroups.js";
import { UpsertGlobalVariationRequest, UpsertGlobalVariationSecurity } from "../models/operations/upsertglobalvariation.js";
export declare class Variations extends ClientSDK {
    /**
     * createGlobal variations
     *
     * @remarks
     * Ensure the project-default (global) tool variation group exists, returning it. Idempotent: returns the existing group unchanged when present, otherwise creates it. Takes no parameters and only manages the single project-default group.
     */
    createGlobal(request?: CreateGlobalToolVariationGroupRequest | undefined, security?: CreateGlobalToolVariationGroupSecurity | undefined, options?: RequestOptions): Promise<ToolVariationGroupResult>;
    /**
     * deleteGlobal variations
     *
     * @remarks
     * Create or update a globally defined tool variation.
     */
    deleteGlobal(request: DeleteGlobalVariationRequest, security?: DeleteGlobalVariationSecurity | undefined, options?: RequestOptions): Promise<DeleteGlobalToolVariationResult>;
    /**
     * listGlobal variations
     *
     * @remarks
     * List globally defined tool variations.
     */
    listGlobal(request?: ListGlobalVariationsRequest | undefined, security?: ListGlobalVariationsSecurity | undefined, options?: RequestOptions): Promise<ListVariationsResult>;
    /**
     * listGroups variations
     *
     * @remarks
     * List the tool variation groups visible to the project. In v1 this returns the project-default group when it exists, or an empty list otherwise.
     */
    listGroups(request?: ListToolVariationGroupsRequest | undefined, security?: ListToolVariationGroupsSecurity | undefined, options?: RequestOptions): Promise<ListToolVariationGroupsResult>;
    /**
     * upsertGlobal variations
     *
     * @remarks
     * Create or update a globally defined tool variation.
     */
    upsertGlobal(request: UpsertGlobalVariationRequest, security?: UpsertGlobalVariationSecurity | undefined, options?: RequestOptions): Promise<UpsertGlobalToolVariationResult>;
}
//# sourceMappingURL=variations.d.ts.map