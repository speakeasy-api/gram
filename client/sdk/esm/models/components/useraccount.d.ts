import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A linked AI account for a user. The identity is (provider, email): the same email registered on two providers is two distinct accounts.
 */
export type UserAccount = {
    /**
     * 'team' (enterprise) or 'personal' (individual); empty when not yet classified
     */
    accountType?: string | undefined;
    /**
     * Email associated with the account; may differ from the user's work email for personal accounts
     */
    email?: string | undefined;
    /**
     * Provider org id for this account; the per-account discriminator used to scope telemetry to this one account
     */
    externalOrgId?: string | undefined;
    /**
     * Account record id (user_accounts.id); used to scope chat/session views to this account
     */
    id?: string | undefined;
    /**
     * Latest activity timestamp for this account in Unix nanoseconds
     */
    lastSeenUnixNano?: string | undefined;
    /**
     * AI provider the account belongs to ('anthropic', 'openai', 'cursor')
     */
    provider: string;
};
/** @internal */
export declare const UserAccount$inboundSchema: z.ZodMiniType<UserAccount, unknown>;
export declare function userAccountFromJSON(jsonString: string): SafeParseResult<UserAccount, SDKValidationError>;
//# sourceMappingURL=useraccount.d.ts.map