import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Assistant } from "../models/components/assistant.js";
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
  GetManagedAssistantRequest,
  GetManagedAssistantSecurity,
} from "../models/operations/getmanagedassistant.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getManagedAssistant assistants
 *
 * @remarks
 * Get the project's built-in Project Assistant if it exists. Returns 404 when no managed assistant has been provisioned yet — call ensureManagedAssistant to create one.
 */
export declare function assistantsGetManaged(
  client: GramCore,
  request?: GetManagedAssistantRequest | undefined,
  security?: GetManagedAssistantSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    Assistant,
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
//# sourceMappingURL=assistantsGetManaged.d.ts.map
