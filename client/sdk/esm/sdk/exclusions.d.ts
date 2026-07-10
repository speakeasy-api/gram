import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListBuiltinExclusionsResult } from "../models/components/listbuiltinexclusionsresult.js";
import { ListRiskExclusionsResult } from "../models/components/listriskexclusionsresult.js";
import { RiskExclusion } from "../models/components/riskexclusion.js";
import { CreateRiskExclusionRequest, CreateRiskExclusionSecurity } from "../models/operations/createriskexclusion.js";
import { DeleteRiskExclusionRequest, DeleteRiskExclusionSecurity } from "../models/operations/deleteriskexclusion.js";
import { ListBuiltinExclusionsRequest, ListBuiltinExclusionsSecurity } from "../models/operations/listbuiltinexclusions.js";
import { ListRiskExclusionsRequest, ListRiskExclusionsSecurity } from "../models/operations/listriskexclusions.js";
import { UpdateRiskExclusionRequest, UpdateRiskExclusionSecurity } from "../models/operations/updateriskexclusion.js";
export declare class Exclusions extends ClientSDK {
    /**
     * createRiskExclusion risk
     *
     * @remarks
     * Create a risk exclusion. Omit risk_policy_id to create a global exclusion that applies to every policy in the project.
     */
    create(request: CreateRiskExclusionRequest, security?: CreateRiskExclusionSecurity | undefined, options?: RequestOptions): Promise<RiskExclusion>;
    /**
     * deleteRiskExclusion risk
     *
     * @remarks
     * Delete a risk exclusion. Previously suppressed findings are restored.
     */
    delete(request: DeleteRiskExclusionRequest, security?: DeleteRiskExclusionSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * listBuiltinExclusions risk
     *
     * @remarks
     * List the built-in exclusion library (known-safe values suppressed before they reach exclusions), grouped by category.
     */
    listBuiltinExclusions(request?: ListBuiltinExclusionsRequest | undefined, security?: ListBuiltinExclusionsSecurity | undefined, options?: RequestOptions): Promise<ListBuiltinExclusionsResult>;
    /**
     * listRiskExclusions risk
     *
     * @remarks
     * List risk exclusions for the current project. Optionally filter to a single policy.
     */
    list(request?: ListRiskExclusionsRequest | undefined, security?: ListRiskExclusionsSecurity | undefined, options?: RequestOptions): Promise<ListRiskExclusionsResult>;
    /**
     * updateRiskExclusion risk
     *
     * @remarks
     * Update a risk exclusion.
     */
    update(request: UpdateRiskExclusionRequest, security?: UpdateRiskExclusionSecurity | undefined, options?: RequestOptions): Promise<RiskExclusion>;
}
//# sourceMappingURL=exclusions.d.ts.map