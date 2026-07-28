import { MutationKey, UseMutationResult } from "@tanstack/react-query";
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
import { MutationHookOptions } from "./_types.js";
export type MintUserSessionMutationVariables = {
  request: MintUserSessionRequest;
  security?: MintUserSessionSecurity | undefined;
  options?: RequestOptions;
};
export type MintUserSessionMutationData = MintUserSessionResponseBody;
export type MintUserSessionMutationError =
  | ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * mintUserSession userSessions
 *
 * @remarks
 * Mint a user_session on behalf of the authenticated dashboard user, bound to an issuer-gated audience: either a toolset (/mcp) or a remote MCP server (/x/mcp). Exactly one of toolset_id or mcp_server_id must be provided. The minted JWT matches the shape /token would emit after a successful OAuth dance, so the runtime MCP gateway validates it through the same path as a real MCP client's bearer.
 */
export declare function useMintUserSessionMutation(
  options?: MutationHookOptions<
    MintUserSessionMutationData,
    MintUserSessionMutationError,
    MintUserSessionMutationVariables
  >,
): UseMutationResult<
  MintUserSessionMutationData,
  MintUserSessionMutationError,
  MintUserSessionMutationVariables
>;
export declare function mutationKeyMintUserSession(): MutationKey;
export declare function buildMintUserSessionMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: MintUserSessionMutationVariables,
  ) => Promise<MintUserSessionMutationData>;
};
//# sourceMappingURL=mintUserSession.d.ts.map
