import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type CreateResponseBody = {
  /**
   * JWT token for chat session
   */
  clientToken: string;
  /**
   * The origin from which the token will be used
   */
  embedOrigin: string;
  /**
   * Token expiration in seconds
   */
  expiresAfter: number;
  /**
   * Session status
   */
  status: string;
  /**
   * User identifier if provided
   */
  userIdentifier?: string | undefined;
};
/** @internal */
export declare const CreateResponseBody$inboundSchema: z.ZodMiniType<
  CreateResponseBody,
  unknown
>;
export declare function createResponseBodyFromJSON(
  jsonString: string,
): SafeParseResult<CreateResponseBody, SDKValidationError>;
//# sourceMappingURL=createresponsebody.d.ts.map
