import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ServerNameOverride } from "../models/components/servernameoverride.js";
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
  UpsertServerNameOverrideRequest,
  UpsertServerNameOverrideSecurity,
} from "../models/operations/upsertservernameoverride.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * upsert hooksServerNames
 *
 * @remarks
 * Create or update a server name display override
 */
export declare function hooksServerNamesUpsertServerNameOverride(
  client: GramCore,
  request: UpsertServerNameOverrideRequest,
  security?: UpsertServerNameOverrideSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ServerNameOverride,
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
//# sourceMappingURL=hooksServerNamesUpsertServerNameOverride.d.ts.map
