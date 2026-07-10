import * as z from "zod/v4-mini";
import { CreateCimdOrganizationRemoteSessionClientForm, CreateCimdOrganizationRemoteSessionClientForm$Outbound } from "../components/createcimdorganizationremotesessionclientform.js";
export type CreateCimdOrganizationRemoteSessionClientSecurity = {
    sessionHeaderGramSession?: string | undefined;
    apikeyHeaderGramKey?: string | undefined;
};
export type CreateCimdOrganizationRemoteSessionClientRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    createCimdOrganizationRemoteSessionClientForm: CreateCimdOrganizationRemoteSessionClientForm;
};
/** @internal */
export type CreateCimdOrganizationRemoteSessionClientSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
    "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const CreateCimdOrganizationRemoteSessionClientSecurity$outboundSchema: z.ZodMiniType<CreateCimdOrganizationRemoteSessionClientSecurity$Outbound, CreateCimdOrganizationRemoteSessionClientSecurity>;
export declare function createCimdOrganizationRemoteSessionClientSecurityToJSON(createCimdOrganizationRemoteSessionClientSecurity: CreateCimdOrganizationRemoteSessionClientSecurity): string;
/** @internal */
export type CreateCimdOrganizationRemoteSessionClientRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    CreateCimdOrganizationRemoteSessionClientForm: CreateCimdOrganizationRemoteSessionClientForm$Outbound;
};
/** @internal */
export declare const CreateCimdOrganizationRemoteSessionClientRequest$outboundSchema: z.ZodMiniType<CreateCimdOrganizationRemoteSessionClientRequest$Outbound, CreateCimdOrganizationRemoteSessionClientRequest>;
export declare function createCimdOrganizationRemoteSessionClientRequestToJSON(createCimdOrganizationRemoteSessionClientRequest: CreateCimdOrganizationRemoteSessionClientRequest): string;
//# sourceMappingURL=createcimdorganizationremotesessionclient.d.ts.map