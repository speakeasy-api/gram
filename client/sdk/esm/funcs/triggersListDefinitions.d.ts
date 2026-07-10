import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListTriggerDefinitionsResult } from "../models/components/listtriggerdefinitionsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListTriggerDefinitionsRequest, ListTriggerDefinitionsSecurity } from "../models/operations/listtriggerdefinitions.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listTriggerDefinitions triggers
 *
 * @remarks
 * List static trigger definitions available to a project.
 */
export declare function triggersListDefinitions(client: GramCore, request?: ListTriggerDefinitionsRequest | undefined, security?: ListTriggerDefinitionsSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListTriggerDefinitionsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=triggersListDefinitions.d.ts.map