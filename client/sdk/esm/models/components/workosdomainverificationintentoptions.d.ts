import * as z from "zod/v4-mini";
export type WorkOSDomainVerificationIntentOptions = {
    /**
     * Domain name to verify.
     */
    domainName?: string | undefined;
};
/** @internal */
export type WorkOSDomainVerificationIntentOptions$Outbound = {
    domain_name?: string | undefined;
};
/** @internal */
export declare const WorkOSDomainVerificationIntentOptions$outboundSchema: z.ZodMiniType<WorkOSDomainVerificationIntentOptions$Outbound, WorkOSDomainVerificationIntentOptions>;
export declare function workOSDomainVerificationIntentOptionsToJSON(workOSDomainVerificationIntentOptions: WorkOSDomainVerificationIntentOptions): string;
//# sourceMappingURL=workosdomainverificationintentoptions.d.ts.map