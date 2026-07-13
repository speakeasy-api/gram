import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { MintUserSessionResponseBody } from "../models/components/mintusersessionresponsebody.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  MintUserSessionRequest,
  MintUserSessionSecurity,
} from "../models/operations/mintusersession.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * mintUserSession userSessions
 *
 * @remarks
 * Mint a user_session on behalf of the authenticated dashboard user, bound to an issuer-gated audience: either a toolset (/mcp) or a remote MCP server (/x/mcp). Exactly one of toolset_id or mcp_server_id must be provided. The minted JWT matches the shape /token would emit after a successful OAuth dance, so the runtime MCP gateway validates it through the same path as a real MCP client's bearer.
 */
export declare function userSessionsMint(
  client: GramCore,
  request: MintUserSessionRequest,
  security?: MintUserSessionSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    MintUserSessionResponseBody,
    | ServiceError
    | GramError
    | ResponseValidationError
    | ConnectionError
    | RequestAbortedError
    | RequestTimeoutError
    | InvalidRequestError
    | UnexpectedClientError
    | SDKValidationError
  >
>;
//# sourceMappingURL=userSessionsMint.d.ts.map
