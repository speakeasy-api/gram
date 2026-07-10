import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreatePromptTemplateResult } from "../models/components/createprompttemplateresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateTemplateRequest, CreateTemplateSecurity } from "../models/operations/createtemplate.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createTemplate templates
 *
 * @remarks
 * Create a new prompt template.
 */
export declare function templatesCreate(client: GramCore, request: CreateTemplateRequest, security?: CreateTemplateSecurity | undefined, options?: RequestOptions): APIPromise<Result<CreatePromptTemplateResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=templatesCreate.d.ts.map