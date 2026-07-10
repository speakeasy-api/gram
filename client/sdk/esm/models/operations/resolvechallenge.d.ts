import * as z from "zod/v4-mini";
import { ResolveChallengeForm, ResolveChallengeForm$Outbound } from "../components/resolvechallengeform.js";
export type ResolveChallengeSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type ResolveChallengeRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    resolveChallengeForm: ResolveChallengeForm;
};
/** @internal */
export type ResolveChallengeSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ResolveChallengeSecurity$outboundSchema: z.ZodMiniType<ResolveChallengeSecurity$Outbound, ResolveChallengeSecurity>;
export declare function resolveChallengeSecurityToJSON(resolveChallengeSecurity: ResolveChallengeSecurity): string;
/** @internal */
export type ResolveChallengeRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    ResolveChallengeForm: ResolveChallengeForm$Outbound;
};
/** @internal */
export declare const ResolveChallengeRequest$outboundSchema: z.ZodMiniType<ResolveChallengeRequest$Outbound, ResolveChallengeRequest>;
export declare function resolveChallengeRequestToJSON(resolveChallengeRequest: ResolveChallengeRequest): string;
//# sourceMappingURL=resolvechallenge.d.ts.map