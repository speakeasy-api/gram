import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ExternalCredentialSummary } from "./externalcredentialsummary.js";
export type ListExternalCredentialsResult = {
    /**
     * The organization's external credentials.
     */
    credentials: Array<ExternalCredentialSummary>;
};
/** @internal */
export declare const ListExternalCredentialsResult$inboundSchema: z.ZodMiniType<ListExternalCredentialsResult, unknown>;
export declare function listExternalCredentialsResultFromJSON(jsonString: string): SafeParseResult<ListExternalCredentialsResult, SDKValidationError>;
//# sourceMappingURL=listexternalcredentialsresult.d.ts.map