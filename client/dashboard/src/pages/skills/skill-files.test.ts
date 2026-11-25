// @vitest-environment node
import { strToU8, zipSync } from "fflate";
import { describe, expect, it } from "vitest";
import {
  diffSkillFiles,
  isLikelyTextFile,
  isSkillScript,
  languageForPath,
  loadDraftFileContent,
  manifestNameFromContent,
  MAX_SKILL_FILE_BYTES,
  MAX_SKILL_FILES,
  parseSkillUpload,
  MAX_SKILL_ZIP_BYTES,
  readSkillFolder,
  readSkillZip,
  readSkillZipFile,
  rootNameWarning,
  type SkillUploadEntry,
  SkillUploadError,
  summarizeFileChanges,
  validateSkillFilePath,
  validateSkillFiles,
} from "./skill-files";

const MANIFEST = "---\nname: pdf-tools\ndescription: Work with PDFs.\n---\n";

function entry(path: string, text = "x"): SkillUploadEntry {
  const content = strToU8(text);
  return { path, content, sizeBytes: content.byteLength };
}

describe("parseSkillUpload", () => {
  it("strips a single wrapping folder and splits out SKILL.md", () => {
    const parsed = parseSkillUpload([
      entry("pdf-tools/SKILL.md", MANIFEST),
      entry("pdf-tools/scripts/extract.py", "print(1)"),
      entry("pdf-tools/references/REFERENCE.md", "# Ref"),
    ]);
    expect(parsed.rootDir).toBe("pdf-tools");
    expect(parsed.manifest).toBe(MANIFEST);
    expect(parsed.files.map((f) => f.path)).toEqual([
      "references/REFERENCE.md",
      "scripts/extract.py",
    ]);
  });

  it("keeps paths as-is when files sit at the top level", () => {
    const parsed = parseSkillUpload([
      entry("SKILL.md", MANIFEST),
      entry("scripts/run.sh"),
    ]);
    expect(parsed.rootDir).toBeNull();
    expect(parsed.files.map((f) => f.path)).toEqual(["scripts/run.sh"]);
  });

  it("drops folder entries and operating system junk", () => {
    const parsed = parseSkillUpload([
      entry("skill/"),
      entry("skill/SKILL.md", MANIFEST),
      entry("skill/.DS_Store"),
      entry("__MACOSX/skill/._SKILL.md"),
      entry("skill/assets/logo.png"),
    ]);
    expect(parsed.rootDir).toBe("skill");
    expect(parsed.files.map((f) => f.path)).toEqual(["assets/logo.png"]);
  });

  it("normalizes Windows separators from zip tools", () => {
    const parsed = parseSkillUpload([
      entry("skill\\SKILL.md", MANIFEST),
      entry("skill\\scripts\\a.ps1"),
    ]);
    expect(parsed.files.map((f) => f.path)).toEqual(["scripts/a.ps1"]);
  });

  it("requires SKILL.md at the top of the skill", () => {
    expect(() =>
      parseSkillUpload([entry("skill/nested/SKILL.md"), entry("other.md")]),
    ).toThrow(SkillUploadError);
    expect(() => parseSkillUpload([])).toThrow("The upload is empty.");
  });

  it("rejects an oversized or non-UTF-8 manifest", () => {
    expect(() =>
      parseSkillUpload([
        { path: "SKILL.md", content: null, sizeBytes: 70_000 },
      ]),
    ).toThrow(/65,536 bytes/);
    expect(() =>
      parseSkillUpload([
        {
          path: "SKILL.md",
          content: new Uint8Array([0xff, 0xfe, 0xfd]),
          sizeBytes: 3,
        },
      ]),
    ).toThrow("SKILL.md is not valid UTF-8.");
  });
});

describe("readSkillZip", () => {
  it("reads entries from a zip archive", () => {
    const zip = zipSync({
      "pdf-tools/SKILL.md": strToU8(MANIFEST),
      "pdf-tools/scripts/extract.py": strToU8("print(1)"),
    });
    const parsed = parseSkillUpload(readSkillZip(zip));
    expect(parsed.rootDir).toBe("pdf-tools");
    expect(parsed.files).toHaveLength(1);
    expect(parsed.files[0]).toMatchObject({
      path: "scripts/extract.py",
      sizeBytes: 8,
    });
  });

  it("does not inflate files over the per-file cap", () => {
    const big = new Uint8Array(MAX_SKILL_FILE_BYTES + 1);
    const zip = zipSync({
      "SKILL.md": strToU8(MANIFEST),
      "assets/big.bin": big,
    });
    const entries = readSkillZip(zip);
    const bigEntry = entries.find((e) => e.path === "assets/big.bin");
    expect(bigEntry?.content).toBeNull();
    expect(bigEntry?.sizeBytes).toBe(MAX_SKILL_FILE_BYTES + 1);
    const parsed = parseSkillUpload(entries);
    expect(validateSkillFiles(parsed.files)).toEqual([
      "assets/big.bin is 5.0 MB. Each file must be 5 MB or smaller.",
    ]);
  });

  it("inflates no supporting files when the set breaks the total limit", () => {
    // Each file is under the per-file cap, but together they exceed 10 MB.
    const chunk = new Uint8Array(4 * 1024 * 1024);
    const zip = zipSync(
      {
        "SKILL.md": strToU8(MANIFEST),
        "assets/a.bin": chunk,
        "assets/b.bin": chunk,
        "assets/c.bin": chunk,
      },
      { level: 0 },
    );
    const entries = readSkillZip(zip);
    expect(entries.find((e) => e.path === "SKILL.md")?.content).not.toBeNull();
    for (const name of ["a", "b", "c"]) {
      const found = entries.find((e) => e.path === `assets/${name}.bin`);
      expect(found?.content).toBeNull();
      expect(found?.sizeBytes).toBe(chunk.byteLength);
    }
    const parsed = parseSkillUpload(entries);
    expect(validateSkillFiles(parsed.files)).toEqual([
      "Supporting files add up to 12 MB. The total must be 10 MB or smaller.",
    ]);
  });

  it("inflates no supporting files when there are too many", () => {
    const files: Record<string, Uint8Array> = {
      "my-skill/SKILL.md": strToU8(MANIFEST),
    };
    for (let i = 0; i <= MAX_SKILL_FILES; i += 1) {
      files[`my-skill/refs/${i}.md`] = strToU8("x");
    }
    const entries = readSkillZip(zipSync(files));
    const parsed = parseSkillUpload(entries);
    expect(parsed.manifest).toBe(MANIFEST);
    expect(parsed.files).toHaveLength(MAX_SKILL_FILES + 1);
    expect(
      parsed.files.every((f) => f.kind === "new" && f.content === null),
    ).toBe(true);
  });

  it("reads deferred entries from the archive once needed", async () => {
    const files: Record<string, Uint8Array> = { "SKILL.md": strToU8(MANIFEST) };
    for (let i = 0; i <= MAX_SKILL_FILES; i += 1) {
      files[`refs/${i}.md`] = strToU8(`ref ${i}`);
    }
    const zip = zipSync(files);
    const archive = {
      size: zip.byteLength,
      arrayBuffer: () => Promise.resolve(zip.slice().buffer),
    };
    const parsed = parseSkillUpload(await readSkillZipFile(archive));
    const ref = parsed.files.find((f) => f.path === "refs/7.md");
    if (ref?.kind !== "new") throw new Error("expected an uploaded file");
    expect(ref.content).toBeNull();
    expect(new TextDecoder().decode(await loadDraftFileContent(ref))).toBe(
      "ref 7",
    );
  });

  it("never returns empty bytes for an unread file", async () => {
    await expect(
      loadDraftFileContent({
        kind: "new",
        path: "assets/a.bin",
        content: null,
        sizeBytes: 4,
      }),
    ).rejects.toThrow("Unable to read assets/a.bin. Upload the skill again.");
  });

  it("refuses an archive over the size cap without reading it", async () => {
    let read = false;
    const file = {
      size: MAX_SKILL_ZIP_BYTES + 1,
      arrayBuffer: () => {
        read = true;
        return Promise.resolve(new ArrayBuffer(0));
      },
    };
    await expect(readSkillZipFile(file)).rejects.toThrow(SkillUploadError);
    expect(read).toBe(false);
  });

  it("reports a file that is not a zip", () => {
    expect(() => readSkillZip(strToU8("not a zip"))).toThrow(
      "The selected file is not a valid .zip archive.",
    );
  });
});

describe("readSkillFolder", () => {
  function pickedFile(path: string, size: number, text = "x") {
    const reads: string[] = [];
    const file = {
      name: path.split("/").pop(),
      webkitRelativePath: path,
      size,
      arrayBuffer: () => {
        reads.push(path);
        return Promise.resolve(strToU8(text).slice().buffer);
      },
    } as unknown as File;
    return { file, reads };
  }

  it("reads the manifest and supporting files within the limits", async () => {
    const manifest = pickedFile("my-skill/SKILL.md", MANIFEST.length, MANIFEST);
    const ref = pickedFile("my-skill/refs/a.md", 1);
    const parsed = parseSkillUpload(
      await readSkillFolder([manifest.file, ref.file]),
    );
    expect(parsed.manifest).toBe(MANIFEST);
    expect(ref.reads).toEqual(["my-skill/refs/a.md"]);
  });

  it("reads only the manifest when the folder breaks the total limit", async () => {
    const manifest = pickedFile("my-skill/SKILL.md", MANIFEST.length, MANIFEST);
    const big = [0, 1, 2].map((i) =>
      pickedFile(`my-skill/assets/${i}.bin`, 4 * 1024 * 1024),
    );
    const entries = await readSkillFolder([
      manifest.file,
      ...big.map((b) => b.file),
    ]);
    expect(manifest.reads).toHaveLength(1);
    expect(big.flatMap((b) => b.reads)).toEqual([]);
    expect(validateSkillFiles(parseSkillUpload(entries).files)).toEqual([
      "Supporting files add up to 12 MB. The total must be 10 MB or smaller.",
    ]);
  });
});

describe("validateSkillFilePath", () => {
  it.each([
    ["scripts/run.sh", null],
    ["", "A file path is empty."],
    ["/etc/passwd", "/etc/passwd must be relative to the skill folder."],
    ["a\\b", "a\\b must use forward slashes."],
    ["../up", "../up points outside the skill folder."],
    ["a//b", 'a//b has an empty or "." folder in its path.'],
    ["./a", './a has an empty or "." folder in its path.'],
    ["skill.md", "skill.md is the skill manifest, not a supporting file."],
    ["a\u0001", "a\u0001 contains a control character."],
  ])("%j", (path, expected) => {
    expect(validateSkillFilePath(path)).toBe(expected);
  });

  it("counts path length in UTF-8 bytes", () => {
    expect(validateSkillFilePath("é".repeat(256))).toBeNull();
    expect(validateSkillFilePath("é".repeat(257))).toMatch(/512 bytes/);
  });
});

describe("validateSkillFiles", () => {
  it("accepts a valid set", () => {
    expect(validateSkillFiles([{ path: "a.md", sizeBytes: 10 }])).toEqual([]);
  });

  it("rejects case-only duplicates", () => {
    expect(
      validateSkillFiles([
        { path: "Ref.md", sizeBytes: 1 },
        { path: "ref.md", sizeBytes: 1 },
      ]),
    ).toEqual([
      "Ref.md and ref.md differ only by letter case. Rename one of them.",
    ]);
  });

  it("enforces the file count and total size caps", () => {
    const many = Array.from({ length: MAX_SKILL_FILES + 1 }, (_, i) => ({
      path: `f${i}.txt`,
      sizeBytes: 1,
    }));
    expect(validateSkillFiles(many)[0]).toMatch(/at most 100 supporting files/);

    const heavy = [0, 1, 2].map((i) => ({
      path: `f${i}.bin`,
      sizeBytes: 4 * 1024 * 1024,
    }));
    expect(validateSkillFiles(heavy)).toEqual([
      "Supporting files add up to 12 MB. The total must be 10 MB or smaller.",
    ]);
  });
});

describe("manifest name", () => {
  it("reads the frontmatter name, unquoting it", () => {
    expect(manifestNameFromContent(MANIFEST)).toBe("pdf-tools");
    expect(manifestNameFromContent('---\nname: "quoted"\n---\n')).toBe(
      "quoted",
    );
    expect(manifestNameFromContent("# no frontmatter")).toBeNull();
    expect(manifestNameFromContent("---\ndescription: x\n---\nname: y")).toBe(
      null,
    );
  });

  it("ignores YAML comments after the name", () => {
    expect(
      manifestNameFromContent("---\nname: pdf-tools # the folder\n---\n"),
    ).toBe("pdf-tools");
    expect(
      manifestNameFromContent("---\nname: 'pdf-tools'  # quoted\n---\n"),
    ).toBe("pdf-tools");
    expect(manifestNameFromContent("---\nname: a#b\n---\n")).toBe("a#b");
    expect(manifestNameFromContent("---\nname: # none\n---\n")).toBeNull();
    expect(
      rootNameWarning("---\nname: pdf-tools # c\n---\n", "pdf-tools"),
    ).toBeNull();
  });

  it("warns only when the folder name differs", () => {
    expect(rootNameWarning(MANIFEST, "pdf-tools")).toBeNull();
    expect(rootNameWarning(MANIFEST, null)).toBeNull();
    expect(rootNameWarning(MANIFEST, "pdf-tools-main")).toContain(
      '"pdf-tools-main"',
    );
  });
});

describe("diffSkillFiles", () => {
  const meta = (path: string, sha256: string) => ({
    path,
    sha256,
    sizeBytes: 1,
    contentType: "text/plain",
  });

  it("classifies files by path and digest", () => {
    const changes = diffSkillFiles(
      [meta("a.md", "1"), meta("b.md", "2"), meta("c.md", "3")],
      [meta("b.md", "2"), meta("c.md", "4"), meta("d.md", "5")],
    );
    expect(changes.map((c) => [c.path, c.status])).toEqual([
      ["a.md", "removed"],
      ["b.md", "unchanged"],
      ["c.md", "changed"],
      ["d.md", "added"],
    ]);
    expect(summarizeFileChanges(changes)).toEqual({
      added: 1,
      removed: 1,
      changed: 1,
    });
  });
});

describe("file kinds", () => {
  it("detects text files from type or extension", () => {
    expect(isLikelyTextFile({ path: "a.bin", contentType: "text/plain" })).toBe(
      true,
    );
    expect(
      isLikelyTextFile({ path: "a.yaml", contentType: "application/yaml" }),
    ).toBe(true);
    expect(
      isLikelyTextFile({
        path: "run.py",
        contentType: "application/octet-stream",
      }),
    ).toBe(true);
    expect(
      isLikelyTextFile({ path: "logo.png", contentType: "image/png" }),
    ).toBe(false);
  });

  it("maps languages and flags scripts", () => {
    expect(languageForPath("scripts/x.py")).toBe("python");
    expect(languageForPath("LICENSE")).toBe("text");
    expect(isSkillScript("scripts/x.py")).toBe(true);
    expect(isSkillScript("references/scripts.md")).toBe(false);
  });
});
