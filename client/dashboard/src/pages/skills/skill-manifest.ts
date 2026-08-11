export const MAX_SKILL_MANIFEST_BYTES = 65_536;

const encoder = new TextEncoder();

export function manifestByteLength(content: string): number {
  return encoder.encode(content).byteLength;
}

export function validateManifestContent(content: string): string | null {
  if (content.length === 0) {
    return "Enter or upload a SKILL.md manifest.";
  }

  const byteLength = manifestByteLength(content);
  if (byteLength > MAX_SKILL_MANIFEST_BYTES) {
    return `SKILL.md must be 65,536 bytes or fewer (currently ${byteLength.toLocaleString()} bytes).`;
  }

  return null;
}

export function decodeManifestFile(bytes: ArrayBuffer): string {
  return new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(
    bytes,
  );
}

export function stripSkillFrontmatter(content: string): string {
  const normalized = content.replace(/^\uFEFF/, "").replace(/\r\n?/g, "\n");
  const lines = normalized.split("\n");

  if (lines[0]?.trimEnd() !== "---") {
    return content;
  }

  const closingIndex = lines.findIndex(
    (line, index) => index > 0 && line.trimEnd() === "---",
  );
  if (closingIndex < 0) {
    return content;
  }

  return lines
    .slice(closingIndex + 1)
    .join("\n")
    .replace(/^\n/, "");
}
