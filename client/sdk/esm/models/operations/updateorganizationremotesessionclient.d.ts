import * as z from "zod/v4-mini";
import { UpdateRemoteSessionClientForm, UpdateRemoteSessionClientForm$Outbound } from "../components/updateremotesessionclientform.js";
export type UpdateOrganizationRemoteSessionClientSecurity = {
    sessionHeaderGramSession?: string | undefined;
    apikeyHeaderGramKey?: string | undefined;
};
export type UpdateOrganizationRemoteSessionClientRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    updateRemoteSessionClientForm: UpdateRemoteSessionClientForm;
};
/** @internal */
export type UpdateOrganizationRemoteSessionClientSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
    "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const UpdateOrganizationRemoteSessionClientSecurity$outboundSchema: z.ZodMiniType<UpdateOrganizationRemoteSessionClientSecurity$Outbound, UpdateOrganizationRemoteSessionClientSecurity>;
export declare function updateOrganizationRemoteSessionClientSecurityToJSON(updateOrganizationRemoteSessionClientSecurity: UpdateOrganizationRemoteSessionClientSecurity): string;
/** @internal */
export type UpdateOrganizationRemoteSessionClientRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    UpdateRemoteSessionClientForm: UpdateRemoteSessionClientForm$Outbound;
};
/** @internal */
export declare const UpdateOrganizationRemoteSessionClientRequest$outboundSchema: z.ZodMiniType<UpdateOrganizationRemoteSessionClientRequest$Outbound, UpdateOrganizationRemoteSessionClientRequest>;
export declare function updateOrganizationRemoteSessionClientRequestToJSON(updateOrganizationRemoteSessionClientRequest: UpdateOrganizationRemoteSessionClientRequest): string;
//# sourceMappingURL=updateorganizationremotesessionclient.d.ts.map