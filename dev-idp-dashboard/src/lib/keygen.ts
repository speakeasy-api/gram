/**
 * Generates an RS256 signing keypair in the browser, for an app that
 * authenticates with private_key_jwt.
 *
 * The private half never leaves the page — only the JWKS is submitted — so
 * there is no path by which this dev-idp could hand someone a key it had
 * already seen. That also means the private key is shown exactly once and
 * cannot be recovered afterwards.
 */

export interface GeneratedKey {
  /** PKCS#8 PEM. This is what the app signs its client assertion with. */
  privateKeyPEM: string;
  /** The JWKS document to register against the app. */
  jwks: string;
  kid: string;
}

const ALGORITHM: RsaHashedKeyGenParams = {
  name: "RSASSA-PKCS1-v1_5",
  modulusLength: 2048,
  publicExponent: new Uint8Array([0x01, 0x00, 0x01]),
  hash: "SHA-256",
};

export async function generateSigningKey(): Promise<GeneratedKey> {
  const pair = await crypto.subtle.generateKey(ALGORITHM, true, [
    "sign",
    "verify",
  ]);

  const pkcs8 = await crypto.subtle.exportKey("pkcs8", pair.privateKey);
  const jwk = (await crypto.subtle.exportKey("jwk", pair.publicKey)) as {
    n?: string;
    e?: string;
  };

  // Same derivation dev-idp uses for its own kid: the SHA-256 of the SPKI DER,
  // base64url. Deterministic from the key, so the same key always presents the
  // same kid.
  const spki = await crypto.subtle.exportKey("spki", pair.publicKey);
  const digest = await crypto.subtle.digest("SHA-256", spki);
  const kid = base64url(digest);

  const jwks = JSON.stringify(
    {
      keys: [
        {
          kty: "RSA",
          use: "sig",
          alg: "RS256",
          kid,
          n: jwk.n ?? "",
          e: jwk.e ?? "",
        },
      ],
    },
    null,
    2,
  );

  return { privateKeyPEM: toPEM(pkcs8), jwks, kid };
}

function toBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

function base64url(buffer: ArrayBuffer): string {
  return toBase64(buffer)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

/** PEM wraps base64 at 64 columns; tools that read PKCS#8 expect that. */
function toPEM(pkcs8: ArrayBuffer): string {
  const body = toBase64(pkcs8)
    .replace(/(.{64})/g, "$1\n")
    .trimEnd();
  return `-----BEGIN PRIVATE KEY-----\n${body}\n-----END PRIVATE KEY-----`;
}
