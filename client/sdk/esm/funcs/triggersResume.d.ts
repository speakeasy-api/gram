import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { TriggerInstance } from "../models/components/triggerinstance.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ResumeTriggerInstanceRequest, ResumeTriggerInstanceSecurity } from "../models/operations/resumetriggerinstance.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * resumeTriggerInstance triggers
 *
 * @remarks
 * Resume a trigger instance.
 */
export declare function triggersResume(client: GramCore, request: ResumeTriggerInstanceRequest, security?: ResumeTriggerInstanceSecurity | undefined, options?: RequestOptions): APIPromise<Result<TriggerInstance, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=triggersResume.d.ts.map