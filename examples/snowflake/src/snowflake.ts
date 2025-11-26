import { createHash, createPrivateKey, createPublicKey } from "crypto";
import jwt from "jsonwebtoken";
import { repairPemKey } from "./util.ts";

export type SnowflakeConfig = {
  accountIdentifier: string;
  username: string;
  privateKey: string;
};

export type ExecuteQueryParams = {
  database: string;
  schema: string;
  warehouse: string;
  role: string;
  statement: string;
  bindings: Record<
    string,
    {
      type:
        | "FIXED"
        | "REAL"
        | "TEXT"
        | "BINARY"
        | "BOOLEAN"
        | "DATE"
        | "TIME"
        | "TIMESTAMP_TZ"
        | "TIMESTAMP_LTZ"
        | "TIMESTAMP_NTZ";
      value: string;
    }
  >;
};

export async function executeQuery(
  cfg: SnowflakeConfig,
  params: ExecuteQueryParams,
) {
  const requestBody = {
    statement: params.statement,
    bindings: params.bindings,
    timeout: 60,
    database: params.database.toUpperCase(),
    schema: params.schema.toUpperCase(),
    warehouse: params.warehouse.toUpperCase(),
    role: params.role.toUpperCase(),
  };

  const bearerToken = buildJwt({
    accountIdentifier: cfg.accountIdentifier.toUpperCase(),
    username: cfg.username.toUpperCase(),
    privateKey: cfg.privateKey,
  });

  const result = await fetch(
    `https://${cfg.accountIdentifier}.snowflakecomputing.com/api/v2/statements`,
    {
      method: "POST",
      headers: {
        Authorization: `Bearer ${bearerToken}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify(requestBody),
    },
  );

  return result.json();
}

function buildJwt(params: {
  privateKey: string;
  username: string;
  accountIdentifier: string;
}) {
  const privateKeyObject = createPrivateKey({
    key: repairPemKey(params.privateKey),
    format: "pem",
  });

  const privateKey = privateKeyObject.export({ format: "pem", type: "pkcs8" });
  const publicKeyObject = createPublicKey({ key: privateKey, format: "pem" });

  const publicKey = publicKeyObject.export({ format: "der", type: "spki" });
  const publicKeyFingerprint =
    "SHA256:" + createHash("sha256").update(publicKey).digest("base64");

  const signOptions = {
    iss: `${params.accountIdentifier}.${params.username}.${publicKeyFingerprint}`,
    sub: `${params.accountIdentifier}.${params.username}`,
    iat: Math.floor(Date.now() / 1000),
    exp: Math.floor(Date.now() / 1000) + 60 * 60,
  };

  var token = jwt.sign(signOptions, privateKey, { algorithm: "RS256" });

  return token;
}
