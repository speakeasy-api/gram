import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type SetAccountTypeSecurity = {
    apikeyHeaderGramKey?: string | undefined;
};
export type SetAccountTypeRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    setAccountTypeRequestBody: components.SetAccountTypeRequestBody;
};
/** @internal */
export type SetAccountTypeSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const SetAccountTypeSecurity$outboundSchema: z.ZodMiniType<SetAccountTypeSecurity$Outbound, SetAccountTypeSecurity>;
export declare function setAccountTypeSecurityToJSON(setAccountTypeSecurity: SetAccountTypeSecurity): string;
/** @internal */
export type SetAccountTypeRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    SetAccountTypeRequestBody: components.SetAccountTypeRequestBody$Outbound;
};
/** @internal */
export declare const SetAccountTypeRequest$outboundSchema: z.ZodMiniType<SetAccountTypeRequest$Outbound, SetAccountTypeRequest>;
export declare function setAccountTypeRequestToJSON(setAccountTypeRequest: SetAccountTypeRequest): string;
//# sourceMappingURL=setaccounttype.d.ts.map