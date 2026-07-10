import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RotateTunneledMcpServerKeyResult } from "../models/components/rotatetunneledmcpserverkeyresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { RotateTunneledMcpServerKeyRequest, RotateTunneledMcpServerKeySecurity } from "../models/operations/rotatetunneledmcpserverkey.js";
import { MutationHookOptions } from "./_types.js";
export type RotateTunneledMcpServerKeyMutationVariables = {
    request: RotateTunneledMcpServerKeyRequest;
    security?: RotateTunneledMcpServerKeySecurity | undefined;
    options?: RequestOptions;
};
export type RotateTunneledMcpServerKeyMutationData = RotateTunneledMcpServerKeyResult;
export type RotateTunneledMcpServerKeyMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * rotateServerKey tunneledMcp
 *
 * @remarks
 * Rotate a tunneled MCP server source key. Returns the new tunnel key once.
 */
export declare function useRotateTunneledMcpServerKeyMutation(options?: MutationHookOptions<RotateTunneledMcpServerKeyMutationData, RotateTunneledMcpServerKeyMutationError, RotateTunneledMcpServerKeyMutationVariables>): UseMutationResult<RotateTunneledMcpServerKeyMutationData, RotateTunneledMcpServerKeyMutationError, RotateTunneledMcpServerKeyMutationVariables>;
export declare function mutationKeyRotateTunneledMcpServerKey(): MutationKey;
export declare function buildRotateTunneledMcpServerKeyMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RotateTunneledMcpServerKeyMutationVariables) => Promise<RotateTunneledMcpServerKeyMutationData>;
};
//# sourceMappingURL=rotateTunneledMcpServerKey.d.ts.map