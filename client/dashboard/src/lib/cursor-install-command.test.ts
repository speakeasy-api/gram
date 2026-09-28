// @vitest-environment node
import { spawnSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { getCursorInstallCommand } from "./cursor-install-command";

const pluginName = "example-observability-cursor";
const archiveName = "observability-cursor (1).zip";
const manifest = JSON.stringify({ name: pluginName, version: "1.0.0" });
const hooks = JSON.stringify({
  version: 1,
  hooks: {
    sessionStart: [
      { command: 'bash "$CURSOR_PLUGIN_ROOT/hooks/bootstrap.sh"' },
    ],
  },
});
type Entry = { name: string; content: string; mode?: number };
const validEntries: Entry[] = [
  { name: ".cursor-plugin/plugin.json", content: manifest },
  { name: "hooks/hooks.json", content: hooks },
  { name: "speakeasy.json", content: "{}" },
  {
    name: "hooks/bootstrap.sh",
    content: "#!/bin/bash\nexit 0\n",
    mode: 0o100755,
  },
];

// All executable commands run with an isolated HOME, never the user's plugin tree.
describe("getCursorInstallCommand", () => {
  let root: string;
  let home: string;
  let parent: string;
  let destination: string;
  let archive: string;

  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "gram-cursor-install-"));
    home = join(root, "home with 'quotes' $and spaces");
    parent = join(home, ".cursor/plugins/local");
    destination = join(parent, pluginName);
    archive = join(home, "Downloads", archiveName);
    mkdirSync(destination, { recursive: true });
    mkdirSync(join(home, "Downloads"), { recursive: true });
    writeFileSync(join(destination, "old-installation"), "keep me");
  });

  afterEach(() => rmSync(root, { recursive: true, force: true }));

  function zip(entries: Entry[] = validEntries) {
    const result = spawnSync(
      "python3",
      [
        "-c",
        `import json, sys, zipfile
with zipfile.ZipFile(sys.argv[1], "w") as archive:
    for entry in json.load(sys.stdin):
        info = zipfile.ZipInfo(entry["name"])
        info.create_system = 3
        info.external_attr = entry.get("mode", 0o100644) << 16
        archive.writestr(info, entry["content"])
`,
        archive,
      ],
      { input: JSON.stringify(entries), encoding: "utf8" },
    );
    expect(result.status, result.stderr).toBe(0);
  }

  function run(
    options: Partial<Parameters<typeof getCursorInstallCommand>[0]> = {},
    path = process.env.PATH,
  ) {
    return spawnSync("/bin/bash", ["--noprofile", "--norc"], {
      input: getCursorInstallCommand({
        pluginName,
        archiveName,
        requireHooks: true,
        ...options,
      }),
      cwd: home,
      env: { HOME: home, PATH: path, LANG: "C" },
      encoding: "utf8",
      timeout: 10_000,
    });
  }

  function expectPreserved() {
    expect(readFileSync(join(destination, "old-installation"), "utf8")).toBe(
      "keep me",
    );
    expect(readdirSync(parent)).toEqual([pluginName]);
  }

  it("preserves the old installation when the ZIP is missing", () => {
    expect(run().status).not.toBe(0);
    expectPreserved();
  });

  it("preserves the old installation when the ZIP is corrupt", () => {
    writeFileSync(archive, "not a zip");
    expect(run().status).not.toBe(0);
    expectPreserved();
  });

  it("replaces a valid flat observability package, preserving executable modes", () => {
    zip();
    const result = run();
    expect(result.status, result.stderr).toBe(0);
    expect(existsSync(join(destination, "old-installation"))).toBe(false);
    expect(
      readFileSync(join(destination, ".cursor-plugin/plugin.json"), "utf8"),
    ).toBe(manifest);
    const script = spawnSync("/bin/bash", [
      "-c",
      'test -x "$1"',
      "test",
      join(destination, "hooks/bootstrap.sh"),
    ]);
    expect(script.status).toBe(0);
    expect(readdirSync(parent)).toEqual([pluginName]);
    expect(existsSync(archive)).toBe(true);
  });

  it("supports a first install", () => {
    rmSync(destination, { recursive: true });
    zip();
    const result = run();
    expect(result.status, result.stderr).toBe(0);
    expect(existsSync(join(destination, "hooks/hooks.json"))).toBe(true);
  });

  it("accepts generic Cursor MCP packages without hooks when not required", () => {
    zip([validEntries[0]!, { name: "mcp.json", content: '{"mcpServers":{}}' }]);
    const result = run({ requireHooks: false });
    expect(result.status, result.stderr).toBe(0);
    expect(existsSync(join(destination, "mcp.json"))).toBe(true);
  });

  it.each([
    ["missing manifest", validEntries.slice(1)],
    [
      "malformed manifest",
      [
        { name: ".cursor-plugin/plugin.json", content: "{" },
        ...validEntries.slice(1),
      ],
    ],
    [
      "wrong manifest name",
      [
        {
          name: ".cursor-plugin/plugin.json",
          content: '{"name":"other","version":"1"}',
        },
        ...validEntries.slice(1),
      ],
    ],
    [
      "missing manifest version",
      [
        {
          name: ".cursor-plugin/plugin.json",
          content: JSON.stringify({ name: pluginName }),
        },
        ...validEntries.slice(1),
      ],
    ],
    [
      "non-object manifest",
      [
        { name: ".cursor-plugin/plugin.json", content: "null" },
        ...validEntries.slice(1),
      ],
    ],
    ["missing required hooks", [validEntries[0]!]],
    [
      "missing bootstrap script",
      validEntries.filter((entry) => entry.name !== "hooks/bootstrap.sh"),
    ],
    [
      "missing runtime config",
      validEntries.filter((entry) => entry.name !== "speakeasy.json"),
    ],
    [
      "malformed hooks",
      [validEntries[0]!, { name: "hooks/hooks.json", content: "{" }],
    ],
    [
      "wrong hook schema",
      [
        validEntries[0]!,
        {
          name: "hooks/hooks.json",
          content: '{"version":1,"hooks":{"sessionStart":[{}]}}',
        },
      ],
    ],
    [
      "extra enclosing folder",
      validEntries.map((entry) => ({
        ...entry,
        name: `wrapper/${entry.name}`,
      })),
    ],
    [
      "path traversal",
      [...validEntries, { name: "../escaped", content: "bad" }],
    ],
    ["absolute path", [...validEntries, { name: "/escaped", content: "bad" }]],
    [
      "symlink",
      [
        ...validEntries,
        { name: "link", content: "../../outside", mode: 0o120777 },
      ],
    ],
  ] satisfies [string, Entry[]][])(
    "rejects %s without touching the installed plugin",
    (_name, entries) => {
      zip(entries);
      expect(run().status).not.toBe(0);
      expectPreserved();
      expect(existsSync(join(parent, "escaped"))).toBe(false);
    },
  );

  it("validates hooks when present even if not required", () => {
    zip([validEntries[0]!, { name: "hooks/hooks.json", content: "null" }]);
    expect(run({ requireHooks: false }).status).not.toBe(0);
    expectPreserved();
  });

  it("rolls back when the final replacement move fails", () => {
    zip();
    const bin = join(root, "bin");
    mkdirSync(bin);
    writeFileSync(
      join(bin, "mv"),
      `#!/bin/bash
case "$1" in */plugin) exit 42 ;; esac
exec /bin/mv "$@"
`,
      { mode: 0o755 },
    );
    const result = run({}, `${bin}:${process.env.PATH}`);
    expect(result.status).toBe(42);
    expectPreserved();
  });

  it("retains the backup and reports its path if rollback also fails", () => {
    zip();
    const bin = join(root, "bin");
    mkdirSync(bin);
    writeFileSync(
      join(bin, "mv"),
      `#!/bin/bash
case "$1" in */plugin|*/previous) exit 42 ;; esac
exec /bin/mv "$@"
`,
      { mode: 0o755 },
    );
    const result = run({}, `${bin}:${process.env.PATH}`);
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("backup retained");
    const stages = readdirSync(parent);
    expect(stages).toHaveLength(1);
    expect(
      readFileSync(
        join(parent, stages[0]!, "previous/old-installation"),
        "utf8",
      ),
    ).toBe("keep me");
  });

  it("refuses to follow a destination symlink", () => {
    zip();
    const outside = join(root, "outside");
    mkdirSync(outside);
    writeFileSync(join(outside, "keep"), "untouched");
    rmSync(destination, { recursive: true });
    symlinkSync(outside, destination);
    expect(run().status).not.toBe(0);
    expect(readFileSync(join(outside, "keep"), "utf8")).toBe("untouched");
  });

  it("fails safely if prerequisites are missing", () => {
    const bin = join(root, "bin");
    mkdirSync(bin);
    symlinkSync("/bin/bash", join(bin, "bash"));
    const result = run({}, bin);
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("Required command not found: unzip");
    expectPreserved();
  });

  it("rejects the known setup template at execution time", () => {
    const result = run({ pluginName: "{{GRAM_CURSOR_PLUGIN_NAME}}" });
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("Resolve the Cursor plugin name");
    expectPreserved();
  });

  it.each([
    "",
    "../escape",
    "/absolute",
    "a/b",
    "a\\b",
    "-option",
    "a;touch injected",
    "$(touch injected)",
    "a'b",
    "name\n",
    "{{UNKNOWN}}",
    "<plugin-slug>",
  ])("rejects unsafe or unresolved slug %j", (name) => {
    expect(() =>
      getCursorInstallCommand({ pluginName: name, archiveName }),
    ).toThrow();
    expectPreserved();
  });

  it.each([
    "../plugin.zip",
    "/tmp/plugin.zip",
    "plugin.zip;touch injected",
    "$(touch injected).zip",
    "plugin'quoted.zip",
    "plugin.zip\n",
    "{{ARCHIVE}}.zip",
  ])("rejects unsafe archive name %j", (name) => {
    expect(() =>
      getCursorInstallCommand({ pluginName, archiveName: name }),
    ).toThrow();
    expectPreserved();
  });
});
