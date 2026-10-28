// Managed sandbox metadata assertions stay in a memory volume, never HOME or logs.
import { mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { dirname } from "node:path";

const metadataURL =
  "http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/identity";
const maxBytes = 16384;
const minimumLifetime = 60;

// Shape/expiry checks prevent publishing unusable responses. The server verifies
// the Google signature and exact admitted issuer/subject/audience on exchange.
export function tokenExpiry(raw, audience, issuer, subject, now = Date.now()) {
  if (typeof raw !== "string" || raw.length > maxBytes)
    throw new Error("invalid metadata assertion");
  const parts = raw.split(".");
  if (
    parts.length !== 3 ||
    parts.some((part) => !/^[A-Za-z0-9_-]+$/.test(part))
  )
    throw new Error("invalid metadata assertion");
  const header = JSON.parse(Buffer.from(parts[0], "base64url"));
  const claims = JSON.parse(Buffer.from(parts[1], "base64url"));
  const seconds = Math.floor(now / 1000);
  if (
    header.alg !== "RS256" ||
    typeof audience !== "string" ||
    !audience ||
    typeof issuer !== "string" ||
    !issuer ||
    typeof subject !== "string" ||
    !subject.startsWith("spiffe://") ||
    claims.iss !== issuer ||
    !(
      claims.aud === audience ||
      (Array.isArray(claims.aud) &&
        claims.aud.length === 1 &&
        claims.aud[0] === audience)
    ) ||
    typeof claims.sub !== "string" ||
    claims.sub !== subject ||
    Object.hasOwn(claims, "cnf") ||
    !Number.isSafeInteger(claims.exp) ||
    !Number.isSafeInteger(claims.iat) ||
    claims.iat > seconds + 30 ||
    claims.exp <= claims.iat ||
    claims.exp - claims.iat > 3600 ||
    claims.exp <= seconds + minimumLifetime
  )
    throw new Error("invalid metadata assertion claims");
  return claims.exp * 1000;
}

export async function fetchAssertion(
  audience,
  issuer,
  subject,
  fetcher = fetch,
  now = Date.now(),
) {
  const url = new URL(metadataURL);
  url.searchParams.set("audience", audience);
  url.searchParams.set("format", "full");
  // google-auth 2.62.0 bind_id_token=False uses GET, with no binding body.
  const response = await fetcher(url, {
    method: "GET",
    headers: { "Metadata-Flavor": "Google" },
    redirect: "error",
    signal: AbortSignal.timeout(10000),
  });
  if (
    !response.ok ||
    response.headers.get("Metadata-Flavor") !== "Google" ||
    !response.body
  )
    throw new Error("metadata unavailable");
  const reader = response.body.getReader();
  const chunks = [];
  let size = 0;
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      size += value.length;
      if (size > maxBytes) throw new Error("metadata response too large");
      chunks.push(value);
    }
  } finally {
    await reader.cancel();
  }
  const raw = Buffer.concat(chunks).toString("utf8").trim();
  return { raw, expiresAt: tokenExpiry(raw, audience, issuer, subject, now) };
}

export async function publishAssertion(path, raw) {
  await mkdir(dirname(path), { recursive: true, mode: 0o700 });
  // A fixed temporary name is safe: each pod has one writer and a private volume.
  const temporary = path + ".next";
  try {
    await writeFile(temporary, raw, { mode: 0o600 });
    await rename(temporary, path);
  } finally {
    await rm(temporary, { force: true });
  }
}

export function refreshDelay(expiresAt, now = Date.now()) {
  // Metadata can return its cached ID token. Retry near expiry without spinning.
  return Math.max(5000, Math.min(300000, expiresAt - now - 300000));
}

export async function ready(path, audience, issuer, subject, now = Date.now()) {
  try {
    tokenExpiry(await readFile(path, "utf8"), audience, issuer, subject, now);
    return true;
  } catch {
    return false;
  }
}

export async function refreshAssertion(
  path,
  audience,
  issuer,
  subject,
  fetcher = fetch,
  now = Date.now(),
) {
  try {
    const token = await fetchAssertion(audience, issuer, subject, fetcher, now);
    await publishAssertion(path, token.raw);
    return { refreshed: true, delay: refreshDelay(token.expiresAt, now) };
  } catch {
    // Remove expired assertions, including leftovers after a sidecar restart.
    if (!(await ready(path, audience, issuer, subject, now)))
      await rm(path, { force: true });
    return { refreshed: false, delay: 5000 };
  }
}

async function main() {
  const audience = process.env.WORKLOAD_TOKEN_AUDIENCE;
  const issuer = process.env.WORKLOAD_TOKEN_ISSUER;
  const subject = process.env.WORKLOAD_TOKEN_SUBJECT;
  const path = process.env.WORKLOAD_TOKEN_PATH;
  if (
    !audience ||
    !issuer ||
    !subject?.startsWith("spiffe://") ||
    !path?.startsWith("/")
  )
    throw new Error("missing configuration");
  if (process.argv.includes("--ready")) {
    process.exitCode = (await ready(path, audience, issuer, subject)) ? 0 : 1;
    return;
  }
  for (;;) {
    const { refreshed, delay } = await refreshAssertion(
      path,
      audience,
      issuer,
      subject,
    );
    // Never print a response, token, URL, subject, or exception from metadata.
    if (!refreshed)
      console.error("Workload assertion refresh unavailable; retrying");
    await new Promise((resolve) => setTimeout(resolve, delay));
  }
}

if (import.meta.main) {
  main().catch(() => {
    console.error("Workload assertion sidecar stopped");
    process.exitCode = 1;
  });
}
