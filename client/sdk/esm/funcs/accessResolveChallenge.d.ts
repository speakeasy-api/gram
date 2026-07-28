import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ResolveChallengesResult } from "../models/components/resolvechallengesresult.js";
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
  ResolveChallengeRequest,
  ResolveChallengeSecurity,
} from "../models/operations/resolvechallenge.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * resolveChallenge access
 *
 * @remarks
 * Record resolutions for one or more denied authz challenges. The caller is responsible for assigning the role first.
 */
export declare function accessResolveChallenge(
  client: GramCore,
  request: ResolveChallengeRequest,
  security?: ResolveChallengeSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ResolveChallengesResult,
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
//# sourceMappingURL=accessResolveChallenge.d.ts.map
