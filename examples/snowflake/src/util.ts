/**
 * Repairs common PEM key formatting issues
 * @param {string} pemKey - The potentially malformed PEM key
 * @returns {string} - Properly formatted PEM key
 */
export function repairPemKey(pemKey: string): string {
  if (!pemKey || typeof pemKey !== "string") {
    throw new Error("Invalid input: pemKey must be a non-empty string");
  }

  let key = pemKey.trim();

  // Step 1: Fix literal \n strings (e.g., "\\n" -> actual newline)
  if (key.includes("\\n")) {
    key = key.replace(/\\n/g, "\n");
  }

  // Step 2: Detect and extract header/footer
  const beginMatches = key.match(/-----BEGIN [A-Z ]+-----/);
  const endMatches = key.match(/-----END [A-Z ]+-----/);

  if (!beginMatches || !endMatches) {
    throw new Error("Invalid PEM key: Missing BEGIN or END markers");
  }

  const header = beginMatches[0];
  const footer = endMatches[0];

  // Step 3: Extract just the key data (remove header, footer, and all whitespace)
  let keyData = key.replace(header, "").replace(footer, "").replace(/\s+/g, ""); // Remove ALL whitespace (spaces, newlines, tabs)

  // Step 4: Check if key is already properly formatted
  // (has newlines every 64 chars approximately)
  const lines = key.split("\n").filter((line) => line.trim().length > 0);
  const hasProperFormatting =
    lines.length > 2 && lines.slice(1, -1).every((line) => line.length <= 64);

  if (hasProperFormatting) {
    // Already properly formatted, just ensure clean structure
    return `${header}\n${lines.slice(1, -1).join("\n")}\n${footer}`;
  }

  // Step 5: Rebuild with proper 64-character line breaks (PEM standard)
  const keyBodyLines = keyData.match(/.{1,64}/g) || [];
  const keyBody = keyBodyLines.join("\n");

  // Step 6: Reconstruct proper PEM format
  const repairedKey = `${header}\n${keyBody}\n${footer}`;

  return repairedKey;
}
