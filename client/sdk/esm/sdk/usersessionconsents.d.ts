import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListUserSessionConsentsRequest, ListUserSessionConsentsResponse, ListUserSessionConsentsSecurity } from "../models/operations/listusersessionconsents.js";
import { RevokeUserSessionConsentRequest, RevokeUserSessionConsentSecurity } from "../models/operations/revokeusersessionconsent.js";
import { PageIterator } from "../types/operations.js";
export declare class UserSessionConsents extends ClientSDK {
    /**
     * listUserSessionConsents userSessionConsents
     *
     * @remarks
     * List consent records for the caller's project.
     */
    list(request?: ListUserSessionConsentsRequest | undefined, security?: ListUserSessionConsentsSecurity | undefined, options?: RequestOptions): Promise<PageIterator<ListUserSessionConsentsResponse, {
        cursor: string;
    }>>;
    /**
     * revokeUserSessionConsent userSessionConsents
     *
     * @remarks
     * Withdraw consent. Subsequent authorization requests for matching (subject, user_session_client) pairs re-prompt.
     */
    revoke(request: RevokeUserSessionConsentRequest, security?: RevokeUserSessionConsentSecurity | undefined, options?: RequestOptions): Promise<void>;
}
//# sourceMappingURL=usersessionconsents.d.ts.map