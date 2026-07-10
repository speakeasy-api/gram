import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListTriggerInstancesResult } from "../models/components/listtriggerinstancesresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListTriggerInstancesRequest, ListTriggerInstancesSecurity } from "../models/operations/listtriggerinstances.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listTriggerInstances triggers
 *
 * @remarks
 * List trigger instances for the current project.
 */
export declare function triggersList(client: GramCore, request?: ListTriggerInstancesRequest | undefined, security?: ListTriggerInstancesSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListTriggerInstancesResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=triggersList.d.ts.map