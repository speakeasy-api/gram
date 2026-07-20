import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type TokenExchangeSecurity = {
  apikeyHeaderGramKey?: string | undefined;
};
export type TokenExchangeRequest = {
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  exchangeRequestBody: components.ExchangeRequestBody;
};
/** @internal */
export type TokenExchangeSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const TokenExchangeSecurity$outboundSchema: z.ZodMiniType<
  TokenExchangeSecurity$Outbound,
  TokenExchangeSecurity
>;
export declare function tokenExchangeSecurityToJSON(
  tokenExchangeSecurity: TokenExchangeSecurity,
): string;
/** @internal */
export type TokenExchangeRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  ExchangeRequestBody: components.ExchangeRequestBody$Outbound;
};
/** @internal */
export declare const TokenExchangeRequest$outboundSchema: z.ZodMiniType<
  TokenExchangeRequest$Outbound,
  TokenExchangeRequest
>;
export declare function tokenExchangeRequestToJSON(
  tokenExchangeRequest: TokenExchangeRequest,
): string;
//# sourceMappingURL=tokenexchange.d.ts.map
