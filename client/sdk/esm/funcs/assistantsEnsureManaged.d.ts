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
  EnsureManagedAssistantRequest,
  EnsureManagedAssistantSecurity,
} from "../models/operations/ensuremanagedassistant.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * ensureManagedAssistant assistants
 *
 * @remarks
 * Get the project's built-in Project Assistant, provisioning it on first access. Idempotent — safe to call on every sidebar open.
 */
export declare function assistantsEnsureManaged(
  client: GramCore,
  request?: EnsureManagedAssistantRequest | undefined,
  security?: EnsureManagedAssistantSecurity | undefined,
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
//# sourceMappingURL=assistantsEnsureManaged.d.ts.map
