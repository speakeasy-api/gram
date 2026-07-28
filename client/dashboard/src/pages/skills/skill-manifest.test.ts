import { describe, expect, it } from "vitest";
import {
  decodeManifestFile,
  manifestByteLength,
  MAX_SKILL_MANIFEST_BYTES,
  stripSkillFrontmatter,
  validateManifestContent,
} from "./skill-manifest";

describe("skill manifest helpers", () => {
  it("counts UTF-8 bytes rather than JavaScript characters", () => {
    expect(manifestByteLength("aé")).toBe(3);
    expect(
      validateManifestContent("é".repeat(MAX_SKILL_MANIFEST_BYTES / 2)),
    ).toBeNull();
    expect(
      validateManifestContent(`a${"é".repeat(MAX_SKILL_MANIFEST_BYTES / 2)}`),
    ).toContain("65,536 bytes or fewer");
  });

  it("decodes valid UTF-8 and fatally rejects invalid bytes", () => {
    expect(decodeManifestFile(new Uint8Array([0x68, 0xc3, 0xa9]).buffer)).toBe(
      "hé",
    );
    expect(() =>
      decodeManifestFile(new Uint8Array([0xc3, 0x28]).buffer),
    ).toThrow();
  });

  it("preserves a UTF-8 BOM for exact raw-content hashing", () => {
    expect(
      decodeManifestFile(
        new Uint8Array([0xef, 0xbb, 0xbf, 0x2d, 0x2d, 0x2d]).buffer,
      ),
    ).toBe("\uFEFF---");
  });

  it("strips BOM and CRLF frontmatter through the first closing delimiter only", () => {
    const manifest =
      "\uFEFF---\r\nname: Example\r\n---\r\n\r\n# Body\r\n\r\n---\r\nkept";
    expect(stripSkillFrontmatter(manifest)).toBe("# Body\n\n---\nkept");
  });

  it("recognizes delimiters with trailing Unicode whitespace without trimming body lines", () => {
    const manifest =
      "--- \u2003\nname: example\ndescription: Example.\n---\t\n\n# Body  \ntext\u2003";
    expect(stripSkillFrontmatter(manifest)).toBe("# Body  \ntext\u2003");
  });

  it("safely returns malformed manifests unchanged", () => {
    expect(stripSkillFrontmatter("# No frontmatter")).toBe("# No frontmatter");
    expect(stripSkillFrontmatter("---\nname: Missing close")).toBe(
      "---\nname: Missing close",
    );
  });
});
