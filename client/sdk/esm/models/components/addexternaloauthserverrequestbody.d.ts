import * as z from "zod/v4-mini";
import { ExternalOAuthServerForm, ExternalOAuthServerForm$Outbound } from "./externaloauthserverform.js";
export type AddExternalOAuthServerRequestBody = {
    externalOauthServer: ExternalOAuthServerForm;
};
/** @internal */
export type AddExternalOAuthServerRequestBody$Outbound = {
    external_oauth_server: ExternalOAuthServerForm$Outbound;
};
/** @internal */
export declare const AddExternalOAuthServerRequestBody$outboundSchema: z.ZodMiniType<AddExternalOAuthServerRequestBody$Outbound, AddExternalOAuthServerRequestBody>;
export declare function addExternalOAuthServerRequestBodyToJSON(addExternalOAuthServerRequestBody: AddExternalOAuthServerRequestBody): string;
//# sourceMappingURL=addexternaloauthserverrequestbody.d.ts.map