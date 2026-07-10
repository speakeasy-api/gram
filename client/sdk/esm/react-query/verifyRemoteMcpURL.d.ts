import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { VerifyURLResult } from "../models/components/verifyurlresult.js";
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
  VerifyRemoteMcpURLRequest,
  VerifyRemoteMcpURLSecurity,
} from "../models/operations/verifyremotemcpurl.js";
import { MutationHookOptions } from "./_types.js";
export type VerifyRemoteMcpURLMutationVariables = {
  request: VerifyRemoteMcpURLRequest;
  security?: VerifyRemoteMcpURLSecurity | undefined;
  options?: RequestOptions;
};
export type VerifyRemoteMcpURLMutationData = VerifyURLResult;
export type VerifyRemoteMcpURLMutationError =
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
 * verifyURL remoteMcp
 *
 * @remarks
 * Probe a candidate remote MCP server URL by issuing an MCP initialize request and reporting the outcome. Used to give users a reachability signal before they save a new or updated remote MCP server. Treats reachable-but-401/403 responses as verified — auth verification is intentionally out of scope.
 */
export declare function useVerifyRemoteMcpURLMutation(
  options?: MutationHookOptions<
    VerifyRemoteMcpURLMutationData,
    VerifyRemoteMcpURLMutationError,
    VerifyRemoteMcpURLMutationVariables
  >,
): UseMutationResult<
  VerifyRemoteMcpURLMutationData,
  VerifyRemoteMcpURLMutationError,
  VerifyRemoteMcpURLMutationVariables
>;
export declare function mutationKeyVerifyRemoteMcpURL(): MutationKey;
export declare function buildVerifyRemoteMcpURLMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: VerifyRemoteMcpURLMutationVariables,
  ) => Promise<VerifyRemoteMcpURLMutationData>;
};
//# sourceMappingURL=verifyRemoteMcpURL.d.ts.map
