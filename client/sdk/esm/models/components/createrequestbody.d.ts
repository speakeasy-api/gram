import * as z from "zod/v4-mini";
export type CreateRequestBody = {
  /**
   * The origin from which the token will be used
   */
  embedOrigin: string;
  /**
   * Token expiration in seconds (max / default 3600)
   */
  expiresAfter?: number | undefined;
  /**
   * Optional free-form user identifier
   */
  userIdentifier?: string | undefined;
};
/** @internal */
export type CreateRequestBody$Outbound = {
  embed_origin: string;
  expires_after: number;
  user_identifier?: string | undefined;
};
/** @internal */
export declare const CreateRequestBody$outboundSchema: z.ZodMiniType<
  CreateRequestBody$Outbound,
  CreateRequestBody
>;
export declare function createRequestBodyToJSON(
  createRequestBody: CreateRequestBody,
): string;
//# sourceMappingURL=createrequestbody.d.ts.map
