import * as z from "zod/v3";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type SlackCallbackRequest = {
  /**
   * The state parameter from the callback
   */
  state: string;
  /**
   * The code parameter from the callback
   */
  code: string;
};
export type SlackCallbackResponse = {
  headers: {
    [k: string]: Array<string>;
  };
};
/** @internal */
export type SlackCallbackRequest$Outbound = {
  state: string;
  code: string;
};
/** @internal */
export declare const SlackCallbackRequest$outboundSchema: z.ZodType<
  SlackCallbackRequest$Outbound,
  z.ZodTypeDef,
  SlackCallbackRequest
>;
export declare function slackCallbackRequestToJSON(
  slackCallbackRequest: SlackCallbackRequest,
): string;
/** @internal */
export declare const SlackCallbackResponse$inboundSchema: z.ZodType<
  SlackCallbackResponse,
  z.ZodTypeDef,
  unknown
>;
export declare function slackCallbackResponseFromJSON(
  jsonString: string,
): SafeParseResult<SlackCallbackResponse, SDKValidationError>;
//# sourceMappingURL=slackcallback.d.ts.map
