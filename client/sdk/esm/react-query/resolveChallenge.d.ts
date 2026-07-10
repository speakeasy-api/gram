import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ResolveChallengesResult } from "../models/components/resolvechallengesresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ResolveChallengeRequest, ResolveChallengeSecurity } from "../models/operations/resolvechallenge.js";
import { MutationHookOptions } from "./_types.js";
export type ResolveChallengeMutationVariables = {
    request: ResolveChallengeRequest;
    security?: ResolveChallengeSecurity | undefined;
    options?: RequestOptions;
};
export type ResolveChallengeMutationData = ResolveChallengesResult;
export type ResolveChallengeMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * resolveChallenge access
 *
 * @remarks
 * Record resolutions for one or more denied authz challenges. The caller is responsible for assigning the role first.
 */
export declare function useResolveChallengeMutation(options?: MutationHookOptions<ResolveChallengeMutationData, ResolveChallengeMutationError, ResolveChallengeMutationVariables>): UseMutationResult<ResolveChallengeMutationData, ResolveChallengeMutationError, ResolveChallengeMutationVariables>;
export declare function mutationKeyResolveChallenge(): MutationKey;
export declare function buildResolveChallengeMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: ResolveChallengeMutationVariables) => Promise<ResolveChallengeMutationData>;
};
//# sourceMappingURL=resolveChallenge.d.ts.map