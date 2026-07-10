import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type CreateSignedChatAttachmentURLResult = {
    /**
     * When the signed URL expires
     */
    expiresAt: Date;
    /**
     * The signed URL to access the chat attachment
     */
    url: string;
};
/** @internal */
export declare const CreateSignedChatAttachmentURLResult$inboundSchema: z.ZodMiniType<CreateSignedChatAttachmentURLResult, unknown>;
export declare function createSignedChatAttachmentURLResultFromJSON(jsonString: string): SafeParseResult<CreateSignedChatAttachmentURLResult, SDKValidationError>;
//# sourceMappingURL=createsignedchatattachmenturlresult.d.ts.map