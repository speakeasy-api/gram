import * as z from "zod/v4-mini";
import {
  OAuthProxyServerForm,
  OAuthProxyServerForm$Outbound,
} from "./oauthproxyserverform.js";
export type AddOAuthProxyServerRequestBody = {
  oauthProxyServer: OAuthProxyServerForm;
};
/** @internal */
export type AddOAuthProxyServerRequestBody$Outbound = {
  oauth_proxy_server: OAuthProxyServerForm$Outbound;
};
/** @internal */
export declare const AddOAuthProxyServerRequestBody$outboundSchema: z.ZodMiniType<
  AddOAuthProxyServerRequestBody$Outbound,
  AddOAuthProxyServerRequestBody
>;
export declare function addOAuthProxyServerRequestBodyToJSON(
  addOAuthProxyServerRequestBody: AddOAuthProxyServerRequestBody,
): string;
//# sourceMappingURL=addoauthproxyserverrequestbody.d.ts.map
