import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListChallengesResult } from "../models/components/listchallengesresult.js";
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
  ListChallengesRequest,
  ListChallengesSecurity,
} from "../models/operations/listchallenges.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listChallenges access
 *
 * @remarks
 * List authz challenge events from ClickHouse, enriched with resolution state from PostgreSQL.
 */
export declare function accessListChallenges(
  client: GramCore,
  request?: ListChallengesRequest | undefined,
  security?: ListChallengesSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListChallengesResult,
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
//# sourceMappingURL=accessListChallenges.d.ts.map
