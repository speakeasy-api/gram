import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { Key } from "../models/components/key.js";
import { ListKeysResult } from "../models/components/listkeysresult.js";
import { ValidateKeyResult } from "../models/components/validatekeyresult.js";
import {
  CreateAPIKeyRequest,
  CreateAPIKeySecurity,
} from "../models/operations/createapikey.js";
import {
  ListAPIKeysRequest,
  ListAPIKeysSecurity,
} from "../models/operations/listapikeys.js";
import {
  RevokeAPIKeyRequest,
  RevokeAPIKeySecurity,
} from "../models/operations/revokeapikey.js";
import {
  ValidateAPIKeyRequest,
  ValidateAPIKeySecurity,
} from "../models/operations/validateapikey.js";
export declare class Keys extends ClientSDK {
  /**
   * createKey keys
   *
   * @remarks
   * Create a new api key
   */
  create(
    request: CreateAPIKeyRequest,
    security?: CreateAPIKeySecurity | undefined,
    options?: RequestOptions,
  ): Promise<Key>;
  /**
   * listKeys keys
   *
   * @remarks
   * List all api keys for an organization
   */
  list(
    request?: ListAPIKeysRequest | undefined,
    security?: ListAPIKeysSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListKeysResult>;
  /**
   * revokeKey keys
   *
   * @remarks
   * Revoke a api key
   */
  revokeById(
    request: RevokeAPIKeyRequest,
    security?: RevokeAPIKeySecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * verifyKey keys
   *
   * @remarks
   * Verify an api key
   */
  validate(
    request?: ValidateAPIKeyRequest | undefined,
    security?: ValidateAPIKeySecurity | undefined,
    options?: RequestOptions,
  ): Promise<ValidateKeyResult>;
}
//# sourceMappingURL=keys.d.ts.map
