import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListRiskEvalReviewsResult } from "../models/components/listriskevalreviewsresult.js";
import { PromptGuardrailEvalResult } from "../models/components/promptguardrailevalresult.js";
import { RiskPolicyEvalReview } from "../models/components/riskpolicyevalreview.js";
import { DeleteRiskEvalReviewRequest, DeleteRiskEvalReviewSecurity } from "../models/operations/deleteriskevalreview.js";
import { EvaluatePromptGuardrailRequest, EvaluatePromptGuardrailSecurity } from "../models/operations/evaluatepromptguardrail.js";
import { ListRiskEvalReviewsRequest, ListRiskEvalReviewsSecurity } from "../models/operations/listriskevalreviews.js";
import { SaveRiskEvalReviewRequest, SaveRiskEvalReviewSecurity } from "../models/operations/saveriskevalreview.js";
export declare class Evals extends ClientSDK {
    /**
     * deleteRiskEvalReview risk
     *
     * @remarks
     * Remove the current reviewer's verdict for one session (the toggle-off path). A reviewer can only clear their own verdict.
     */
    deleteReview(request: DeleteRiskEvalReviewRequest, security?: DeleteRiskEvalReviewSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * evaluatePromptGuardrail risk
     *
     * @remarks
     * Replay a prompt_based guardrail against a single chat session and return the LLM judge's per-message verdict. The guardrail (prompt + judge config + message-type scope + CEL scope) is passed inline so the policy-eval workbench can evaluate an unsaved draft before a policy exists. This path is read-only: it never writes risk_results, publishes to the outbox, or enforces. It exists purely to tune a guardrail against real transcripts. Judges only the chat's latest generation; message-type scoping and CEL scope predicates are both applied.
     */
    evaluate(request: EvaluatePromptGuardrailRequest, security?: EvaluatePromptGuardrailSecurity | undefined, options?: RequestOptions): Promise<PromptGuardrailEvalResult>;
    /**
     * listRiskEvalReviews risk
     *
     * @remarks
     * List the active regression set for a prompt-based policy: every reviewer's current ground-truth verdicts.
     */
    listReviews(request: ListRiskEvalReviewsRequest, security?: ListRiskEvalReviewsSecurity | undefined, options?: RequestOptions): Promise<ListRiskEvalReviewsResult>;
    /**
     * saveRiskEvalReview risk
     *
     * @remarks
     * Record (or replace) the current reviewer's ground-truth verdict for one chat session under a prompt-based policy. This is the durable regression set the eval workbench scores the live guardrail against. Upserts: a reviewer has at most one verdict per session per policy.
     */
    saveReview(request: SaveRiskEvalReviewRequest, security?: SaveRiskEvalReviewSecurity | undefined, options?: RequestOptions): Promise<RiskPolicyEvalReview>;
}
//# sourceMappingURL=evals.d.ts.map