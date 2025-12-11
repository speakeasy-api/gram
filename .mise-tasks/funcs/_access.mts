import crypto from "node:crypto";

export async function mintV1Bearer(encryptionKey: string): Promise<string> {
  const keyBytes = Buffer.from(encryptionKey, "base64");

  const payload = JSON.stringify({
    id: crypto.randomUUID(),
    exp: Math.trunc((Date.now() + 2 * 60 * 60 * 1000) / 1000),
  });

  const encoder = new TextEncoder();
  const plaintext = encoder.encode(payload);

  const key = await crypto.subtle.importKey(
    "raw",
    keyBytes,
    { name: "AES-GCM" },
    false,
    ["encrypt"],
  );

  // Generate random nonce (12 bytes for GCM)
  const nonce = crypto.getRandomValues(new Uint8Array(12));

  // Encrypt the payload
  const ciphertext = await crypto.subtle.encrypt(
    { name: "AES-GCM", iv: nonce },
    key,
    plaintext,
  );

  // Prepend nonce to ciphertext (matching Go's gcm.Seal behavior)
  const combined = new Uint8Array(nonce.length + ciphertext.byteLength);
  combined.set(nonce, 0);
  combined.set(new Uint8Array(ciphertext), nonce.length);

  // Base64 encode the result
  const encrypted = Buffer.from(combined).toString("base64");

  return `v01.${encrypted}`;
}
