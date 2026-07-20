import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Key } from "./key.js";
export type ListKeysResult = {
  keys: Array<Key>;
};
/** @internal */
export declare const ListKeysResult$inboundSchema: z.ZodMiniType<
  ListKeysResult,
  unknown
>;
export declare function listKeysResultFromJSON(
  jsonString: string,
): SafeParseResult<ListKeysResult, SDKValidationError>;
//# sourceMappingURL=listkeysresult.d.ts.map
