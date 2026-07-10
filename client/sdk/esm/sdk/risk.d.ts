import { ClientSDK } from "../lib/sdks.js";
import { Blocks } from "./blocks.js";
import { Categories } from "./categories.js";
import { CustomRules } from "./customrules.js";
import { Evals } from "./evals.js";
import { Exclusions } from "./exclusions.js";
import { Expr } from "./expr.js";
import { Overview } from "./overview.js";
import { Policies } from "./policies.js";
import { PolicyBypassRequests } from "./policybypassrequests.js";
import { Results } from "./results.js";
import { Rules } from "./rules.js";
export declare class Risk extends ClientSDK {
    private _policyBypassRequests?;
    get policyBypassRequests(): PolicyBypassRequests;
    private _expr?;
    get expr(): Expr;
    private _customRules?;
    get customRules(): CustomRules;
    private _exclusions?;
    get exclusions(): Exclusions;
    private _policies?;
    get policies(): Policies;
    private _blocks?;
    get blocks(): Blocks;
    private _overview?;
    get overview(): Overview;
    private _categories?;
    get categories(): Categories;
    private _results?;
    get results(): Results;
    private _rules?;
    get rules(): Rules;
    private _evals?;
    get evals(): Evals;
}
//# sourceMappingURL=risk.d.ts.map