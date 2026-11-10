import { spawnSync } from "node:child_process";
import { X509Certificate } from "node:crypto";
import { readFileSync, readdirSync, statSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { rootCertificates } from "node:tls";

// DER SHA-256 fingerprints from the incident's published roots.json.
export const distrustedRoots = new Set([
  "7dd78d5f4f13459a83dff9abbba62edbaf6f2d102bf257fd712f4d9f2746ed8d",
  "f2edaaff3d9b701fb4d14c9f285f8c0bad90fc3bcc1fa07d47f4718edaba34a5",
  "d7a7a0fb5d7e2731d771e9484ebcdef71d5f0c3e0a2948782bc83ee0ea699ef4",
  "86a1ecba089c4a8d3bbe2734c612ba341d813e043cf9e8a862cd5c57a36bbe6b",
]);

export function fingerprint(pem) {
  return new X509Certificate(pem).fingerprint256
    .replaceAll(":", "")
    .toLowerCase();
}

export function verifyBundle(pem, blocked = distrustedRoots) {
  const certificates = pem.match(
    /-----BEGIN CERTIFICATE-----[\s\S]*?-----END CERTIFICATE-----/g,
  );
  if (!certificates?.length)
    throw new Error("CA bundle contains no certificates");
  for (const certificate of certificates) {
    if (blocked.has(fingerprint(certificate))) {
      throw new Error("CA bundle contains a distrusted root");
    }
  }
}

export function bunBundle(
  systemBundle,
  bundled = rootCertificates,
  blocked = distrustedRoots,
) {
  verifyBundle(systemBundle, blocked);
  let result = systemBundle + "\n";
  for (const certificate of bundled) {
    if (!blocked.has(fingerprint(certificate))) continue;
    // Bun loads OpenSSL's file before adding its bundled roots. Explicit
    // rejection entries take precedence over duplicates in the bundled set.
    // Ordinary PEM removal alone cannot remove Bun's embedded trust anchors.
    const rejection = spawnSync(
      "openssl",
      [
        "x509",
        "-addreject",
        "serverAuth",
        "-addreject",
        "clientAuth",
        "-trustout",
      ],
      { input: certificate, encoding: "utf8" },
    );
    if (
      rejection.status !== 0 ||
      !rejection.stdout.includes("BEGIN TRUSTED CERTIFICATE")
    ) {
      throw new Error("Could not encode CA rejection", {
        cause: rejection.error,
      });
    }
    result += rejection.stdout;
  }
  return result;
}

if (import.meta.main) {
  const [systemFile, certDirectory, outputFile] = process.argv.slice(2);
  if (!systemFile || !certDirectory || !outputFile) {
    throw new Error(
      "Usage: ca-bundle.mjs <system-bundle> <cert-directory> <bun-bundle>",
    );
  }
  // Check the regenerated contents, including OpenSSL's directory lookup.
  // A renamed distro entry must fail the build rather than bypass the sed filter.
  for (const name of readdirSync(certDirectory)) {
    const path = join(certDirectory, name);
    if (!statSync(path).isFile()) continue;
    const contents = readFileSync(path, "utf8");
    if (contents.includes("-----BEGIN CERTIFICATE-----"))
      verifyBundle(contents);
  }
  writeFileSync(outputFile, bunBundle(readFileSync(systemFile, "utf8")), {
    mode: 0o644,
  });
}
