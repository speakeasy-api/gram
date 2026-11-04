import assert from "node:assert/strict";
import { createPublicKey, verify } from "node:crypto";
import { test } from "node:test";
import {
  validateWorkloadHostname,
  validateWorkloadAddresses,
  workloadSigner,
} from "./workload-issuer.ts";

test("workload assertions use an ephemeral RS256 key and exact issuer, subject and audience", () => {
  const signer = workloadSigner();
  const token = signer.issue(
    "https://issuer.example",
    "fixture:runner",
    "https://server.example/oauth",
  );
  const [header, payload, signature] = token.split(".") as [
    string,
    string,
    string,
  ];
  assert.deepEqual(JSON.parse(Buffer.from(header, "base64url").toString()), {
    alg: "RS256",
    typ: "JWT",
    kid: signer.jwk.kid,
  });
  const claims = JSON.parse(Buffer.from(payload, "base64url").toString());
  assert.equal(claims.iss, "https://issuer.example");
  assert.equal(claims.sub, "fixture:runner");
  assert.equal(claims.aud, "https://server.example/oauth");
  assert.equal(claims.exp - claims.iat, 120);
  assert.equal(typeof claims.jti, "string");
  const key = createPublicKey({ key: signer.jwk, format: "jwk" });
  assert.ok(
    verify(
      "RSA-SHA256",
      Buffer.from(`${header}.${payload}`),
      key,
      Buffer.from(signature, "base64url"),
    ),
  );
  const tampered = Buffer.from(
    JSON.stringify({ ...claims, sub: "fixture:other" }),
  ).toString("base64url");
  assert.equal(
    verify(
      "RSA-SHA256",
      Buffer.from(`${header}.${tampered}`),
      key,
      Buffer.from(signature, "base64url"),
    ),
    false,
  );
  assert.equal("d" in signer.jwk, false);
  assert.notEqual(token, signer.issue(claims.iss, claims.sub, claims.aud));
});

test("independent workload issuers cannot sign for each other's JWKS", () => {
  const first = workloadSigner();
  const second = workloadSigner();
  const [header, payload, signature] = first
    .issue(
      "https://issuer.example",
      "fixture:runner",
      "https://server.example/oauth",
    )
    .split(".") as [string, string, string];
  assert.notEqual(first.jwk.kid, second.jwk.kid);
  assert.equal(
    verify(
      "RSA-SHA256",
      Buffer.from(`${header}.${payload}`),
      createPublicKey({ key: second.jwk, format: "jwk" }),
      Buffer.from(signature, "base64url"),
    ),
    false,
  );
});

test("workload TLS host must be a domain, not an IP or URL", () => {
  for (const host of [
    "127.0.0.1.sslip.io",
    "workload.localhost",
    "issuer.example",
  ]) {
    assert.doesNotThrow(() => validateWorkloadHostname(host));
  }
  for (const host of [
    "127.0.0.1",
    "::1",
    "localhost",
    "https://issuer.example",
    "issuer.example:443",
    ".example",
    "issuer..example",
    "-issuer.example",
    "issuer_.example",
    "issuer.example.",
  ]) {
    assert.throws(() => validateWorkloadHostname(host), /ENV_WORKLOAD_ISSUER/);
  }
});

test("workload DNS rejects missing, mixed, IPv6 and non-loopback answers", () => {
  assert.doesNotThrow(() =>
    validateWorkloadAddresses([{ address: "127.0.0.1" }]),
  );
  for (const addresses of [
    [],
    [{ address: "::1" }],
    [{ address: "127.0.0.2" }],
    [{ address: "192.0.2.1" }],
    [{ address: "127.0.0.1" }, { address: "192.0.2.1" }],
  ]) {
    assert.throws(
      () => validateWorkloadAddresses(addresses),
      /ENV_WORKLOAD_ISSUER/,
    );
  }
});
