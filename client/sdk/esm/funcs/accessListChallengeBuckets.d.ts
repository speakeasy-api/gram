import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListChallengeBucketsResult } from "../models/components/listchallengebucketsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListChallengeBucketsRequest, ListChallengeBucketsSecurity } from "../models/operations/listchallengebuckets.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listChallengeBuckets access
 *
 * @remarks
 * List authz challenges grouped into time-based burst buckets. Consecutive challenges with the same dimensions within a 10-minute window are collapsed into a single bucket.
 */
export declare function accessListChallengeBuckets(client: GramCore, request?: ListChallengeBucketsRequest | undefined, security?: ListChallengeBucketsSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListChallengeBucketsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=accessListChallengeBuckets.d.ts.map