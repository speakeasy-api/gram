import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * The new account tier.
 */
export declare const SetAccountTypeRequestBodyGramAccountType: {
    readonly Free: "free";
    readonly Pro: "pro";
    readonly Enterprise: "enterprise";
};
/**
 * The new account tier.
 */
export type SetAccountTypeRequestBodyGramAccountType = ClosedEnum<typeof SetAccountTypeRequestBodyGramAccountType>;
export type SetAccountTypeRequestBody = {
    /**
     * The new account tier.
     */
    gramAccountType: SetAccountTypeRequestBodyGramAccountType;
    /**
     * The Gram organization ID to update.
     */
    organizationId: string;
};
/** @internal */
export declare const SetAccountTypeRequestBodyGramAccountType$outboundSchema: z.ZodMiniEnum<typeof SetAccountTypeRequestBodyGramAccountType>;
/** @internal */
export type SetAccountTypeRequestBody$Outbound = {
    gram_account_type: string;
    organization_id: string;
};
/** @internal */
export declare const SetAccountTypeRequestBody$outboundSchema: z.ZodMiniType<SetAccountTypeRequestBody$Outbound, SetAccountTypeRequestBody>;
export declare function setAccountTypeRequestBodyToJSON(setAccountTypeRequestBody: SetAccountTypeRequestBody): string;
//# sourceMappingURL=setaccounttyperequestbody.d.ts.map