import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { WorkOSIntentOptions, WorkOSIntentOptions$Outbound } from "./workosintentoptions.js";
/**
 * WorkOS Admin Portal intent.
 */
export declare const Intent: {
    readonly Dsync: "dsync";
    readonly Sso: "sso";
    readonly AuditLogs: "audit_logs";
    readonly DomainVerification: "domain_verification";
    readonly LogStreams: "log_streams";
};
/**
 * WorkOS Admin Portal intent.
 */
export type Intent = ClosedEnum<typeof Intent>;
export type GenerateWorkOSAdminPortalLinkRequestBody = {
    /**
     * WorkOS Admin Portal intent.
     */
    intent: Intent;
    intentOptions?: WorkOSIntentOptions | undefined;
    /**
     * IT contact email addresses displayed in the Admin Portal for end-user support.
     */
    itContactEmails?: Array<string> | undefined;
    /**
     * URL to redirect the user to after the Admin Portal session ends.
     */
    returnUrl?: string | undefined;
    /**
     * URL to redirect the user to on successful completion of the Admin Portal flow.
     */
    successUrl?: string | undefined;
};
/** @internal */
export declare const Intent$outboundSchema: z.ZodMiniEnum<typeof Intent>;
/** @internal */
export type GenerateWorkOSAdminPortalLinkRequestBody$Outbound = {
    intent: string;
    intent_options?: WorkOSIntentOptions$Outbound | undefined;
    it_contact_emails?: Array<string> | undefined;
    return_url?: string | undefined;
    success_url?: string | undefined;
};
/** @internal */
export declare const GenerateWorkOSAdminPortalLinkRequestBody$outboundSchema: z.ZodMiniType<GenerateWorkOSAdminPortalLinkRequestBody$Outbound, GenerateWorkOSAdminPortalLinkRequestBody>;
export declare function generateWorkOSAdminPortalLinkRequestBodyToJSON(generateWorkOSAdminPortalLinkRequestBody: GenerateWorkOSAdminPortalLinkRequestBody): string;
//# sourceMappingURL=generateworkosadminportallinkrequestbody.d.ts.map