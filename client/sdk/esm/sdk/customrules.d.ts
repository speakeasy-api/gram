import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListCustomDetectionRulesResult } from "../models/components/listcustomdetectionrulesresult.js";
import { RiskCustomDetectionRule } from "../models/components/riskcustomdetectionrule.js";
import { SuggestCustomDetectionRuleResult } from "../models/components/suggestcustomdetectionruleresult.js";
import { CreateCustomDetectionRuleRequest, CreateCustomDetectionRuleSecurity } from "../models/operations/createcustomdetectionrule.js";
import { DeleteCustomDetectionRuleRequest, DeleteCustomDetectionRuleSecurity } from "../models/operations/deletecustomdetectionrule.js";
import { GetCustomDetectionRuleRequest, GetCustomDetectionRuleSecurity } from "../models/operations/getcustomdetectionrule.js";
import { ListCustomDetectionRulesRequest, ListCustomDetectionRulesSecurity } from "../models/operations/listcustomdetectionrules.js";
import { SuggestCustomDetectionRuleRequest, SuggestCustomDetectionRuleSecurity } from "../models/operations/suggestcustomdetectionrule.js";
import { UpdateCustomDetectionRuleRequest, UpdateCustomDetectionRuleSecurity } from "../models/operations/updatecustomdetectionrule.js";
export declare class CustomRules extends ClientSDK {
    /**
     * createCustomDetectionRule risk
     *
     * @remarks
     * Create a custom regex-backed detection rule for the current project.
     */
    create(request: CreateCustomDetectionRuleRequest, security?: CreateCustomDetectionRuleSecurity | undefined, options?: RequestOptions): Promise<RiskCustomDetectionRule>;
    /**
     * deleteCustomDetectionRule risk
     *
     * @remarks
     * Delete a custom detection rule.
     */
    delete(request: DeleteCustomDetectionRuleRequest, security?: DeleteCustomDetectionRuleSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * getCustomDetectionRule risk
     *
     * @remarks
     * Get a custom detection rule by ID.
     */
    get(request: GetCustomDetectionRuleRequest, security?: GetCustomDetectionRuleSecurity | undefined, options?: RequestOptions): Promise<RiskCustomDetectionRule>;
    /**
     * listCustomDetectionRules risk
     *
     * @remarks
     * List custom detection rules for the current project.
     */
    list(request?: ListCustomDetectionRulesRequest | undefined, security?: ListCustomDetectionRulesSecurity | undefined, options?: RequestOptions): Promise<ListCustomDetectionRulesResult>;
    /**
     * suggestCustomDetectionRule risk
     *
     * @remarks
     * Suggest a custom detection rule (rule_id, title, description, regex, severity) from a natural-language prompt. Calls the configured LLM with a JSON-schema constrained response so the dashboard can prefill the create form.
     */
    suggest(request: SuggestCustomDetectionRuleRequest, security?: SuggestCustomDetectionRuleSecurity | undefined, options?: RequestOptions): Promise<SuggestCustomDetectionRuleResult>;
    /**
     * updateCustomDetectionRule risk
     *
     * @remarks
     * Update a custom detection rule.
     */
    update(request: UpdateCustomDetectionRuleRequest, security?: UpdateCustomDetectionRuleSecurity | undefined, options?: RequestOptions): Promise<RiskCustomDetectionRule>;
}
//# sourceMappingURL=customrules.d.ts.map