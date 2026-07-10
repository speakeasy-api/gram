import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { PeriodUsage } from "../models/components/periodusage.js";
import { TokensUnderManagement } from "../models/components/tokensundermanagement.js";
import { UsageTiers } from "../models/components/usagetiers.js";
import { CreateCheckoutRequest, CreateCheckoutSecurity } from "../models/operations/createcheckout.js";
import { CreateCustomerSessionRequest, CreateCustomerSessionSecurity } from "../models/operations/createcustomersession.js";
import { CreateTopUpCheckoutRequest, CreateTopUpCheckoutSecurity } from "../models/operations/createtopupcheckout.js";
import { GetPeriodUsageRequest, GetPeriodUsageSecurity } from "../models/operations/getperiodusage.js";
import { GetTokensUnderManagementRequest, GetTokensUnderManagementSecurity } from "../models/operations/gettokensundermanagement.js";
import { SetBillingMetadataRequest, SetBillingMetadataSecurity } from "../models/operations/setbillingmetadata.js";
export declare class Usage extends ClientSDK {
    /**
     * createCheckout usage
     *
     * @remarks
     * Create a checkout link for upgrading to the business plan
     */
    createCheckout(request?: CreateCheckoutRequest | undefined, security?: CreateCheckoutSecurity | undefined, options?: RequestOptions): Promise<string>;
    /**
     * createCustomerSession usage
     *
     * @remarks
     * Create a customer session for the user
     */
    createCustomerSession(request?: CreateCustomerSessionRequest | undefined, security?: CreateCustomerSessionSecurity | undefined, options?: RequestOptions): Promise<string>;
    /**
     * createTopUpCheckout usage
     *
     * @remarks
     * Create a checkout link for a one-time credit top-up purchase
     */
    createTopUpCheckout(request?: CreateTopUpCheckoutRequest | undefined, security?: CreateTopUpCheckoutSecurity | undefined, options?: RequestOptions): Promise<string>;
    /**
     * getPeriodUsage usage
     *
     * @remarks
     * Get the usage for an organization for a given period
     */
    getPeriodUsage(request?: GetPeriodUsageRequest | undefined, security?: GetPeriodUsageSecurity | undefined, options?: RequestOptions): Promise<PeriodUsage>;
    /**
     * getTokensUnderManagement usage
     *
     * @remarks
     * Get tokens under management for the active billing cycle alongside the contracted terms
     */
    getTokensUnderManagement(request?: GetTokensUnderManagementRequest | undefined, security?: GetTokensUnderManagementSecurity | undefined, options?: RequestOptions): Promise<TokensUnderManagement>;
    /**
     * getUsageTiers usage
     *
     * @remarks
     * Get the usage tiers
     */
    getUsageTiers(options?: RequestOptions): Promise<UsageTiers>;
    /**
     * setBillingMetadata usage
     *
     * @remarks
     * Set an organization's billing contract terms. Restricted to platform admins.
     */
    setBillingMetadata(request: SetBillingMetadataRequest, security?: SetBillingMetadataSecurity | undefined, options?: RequestOptions): Promise<TokensUnderManagement>;
}
//# sourceMappingURL=usage.d.ts.map