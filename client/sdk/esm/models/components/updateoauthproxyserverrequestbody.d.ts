import * as z from "zod/v4-mini";
import {
  OAuthProxyServerUpdateForm,
  OAuthProxyServerUpdateForm$Outbound,
} from "./oauthproxyserverupdateform.js";
export type UpdateOAuthProxyServerRequestBody = {
  oauthProxyServer: OAuthProxyServerUpdateForm;
};
/** @internal */
export type UpdateOAuthProxyServerRequestBody$Outbound = {
  oauth_proxy_server: OAuthProxyServerUpdateForm$Outbound;
};
/** @internal */
export declare const UpdateOAuthProxyServerRequestBody$outboundSchema: z.ZodMiniType<
  UpdateOAuthProxyServerRequestBody$Outbound,
  UpdateOAuthProxyServerRequestBody
>;
export declare function updateOAuthProxyServerRequestBodyToJSON(
  updateOAuthProxyServerRequestBody: UpdateOAuthProxyServerRequestBody,
): string;
//# sourceMappingURL=updateoauthproxyserverrequestbody.d.ts.map
