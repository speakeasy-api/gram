import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  realpathSync,
  rmSync,
  statSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const packageDir = realpathSync(fileURLToPath(new URL("..", import.meta.url)));
const workspaceDir = dirname(packageDir);

const exportContracts = [
  {
    specifier: "@gram-ai/elements",
    source: "src/index.ts",
    defaultTarget: "dist/elements.js",
    types: "dist/index.d.ts",
  },
  {
    specifier: "@gram-ai/elements/server",
    source: "src/server.ts",
    defaultTarget: "dist/server.js",
    types: "dist/server.d.ts",
  },
  {
    specifier: "@gram-ai/elements/server/core",
    source: "src/server/core.ts",
    defaultTarget: "dist/server/core.js",
    types: "dist/server/core.d.ts",
  },
  {
    specifier: "@gram-ai/elements/server/express",
    source: "src/server/express.ts",
    defaultTarget: "dist/server/express.js",
    types: "dist/server/express.d.ts",
  },
  {
    specifier: "@gram-ai/elements/server/nextjs",
    source: "src/server/nextjs.ts",
    defaultTarget: "dist/server/nextjs.js",
    types: "dist/server/nextjs.d.ts",
  },
  {
    specifier: "@gram-ai/elements/server/fastify",
    source: "src/server/fastify.ts",
    defaultTarget: "dist/server/fastify.js",
    types: "dist/server/fastify.d.ts",
  },
  {
    specifier: "@gram-ai/elements/server/hono",
    source: "src/server/hono.ts",
    defaultTarget: "dist/server/hono.js",
    types: "dist/server/hono.d.ts",
  },
  {
    specifier: "@gram-ai/elements/server/bun",
    source: "src/server/bun.ts",
    defaultTarget: "dist/server/bun.js",
    types: "dist/server/bun.d.ts",
  },
  {
    specifier: "@gram-ai/elements/server/tanstack-start",
    source: "src/server/tanstack-start.ts",
    defaultTarget: "dist/server/tanstack-start.js",
    types: "dist/server/tanstack-start.d.ts",
  },
  {
    specifier: "@gram-ai/elements/plugins",
    source: "src/plugins/index.ts",
    defaultTarget: "dist/plugins.js",
    types: "dist/plugins/index.d.ts",
  },
  {
    specifier: "@gram-ai/elements/compat",
    source: "src/compat-plugin.ts",
    defaultTarget: "dist/compat-plugin.js",
    types: "dist/compat-plugin.d.ts",
  },
  {
    specifier: "@gram-ai/elements/elements.css",
    source: "src/global.css",
    defaultTarget: "dist/elements.css",
  },
];

const tempDir = mkdtempSync(join(tmpdir(), "gram-elements-contract-"));

try {
  const tarball = join(tempDir, "gram-ai-elements.tgz");
  execFileSync(
    "pnpm",
    ["--config.ignore-scripts=true", "pack", "--out", tarball],
    { cwd: packageDir, stdio: "pipe" },
  );

  const unpackedDir = join(tempDir, "unpacked");
  mkdirSync(unpackedDir);
  execFileSync("tar", ["-xzf", tarball, "-C", unpackedDir]);
  const packedPackageDir = join(unpackedDir, "package");

  for (const contract of exportContracts) {
    for (const target of [
      contract.source,
      contract.defaultTarget,
      contract.types,
    ]) {
      if (target) {
        assert.ok(
          existsSync(join(packedPackageDir, target)),
          `packed artifact is missing ${target}`,
        );
      }
    }
  }
  assert.ok(
    existsSync(join(packedPackageDir, "bin/cli.js")),
    "packed artifact is missing bin/cli.js",
  );
  const builtCss = readFileSync(
    join(packedPackageDir, "dist/elements.css"),
    "utf8",
  );
  assert.match(builtCss, /\.gram-elements/, "built CSS is missing its scope");
  assert.match(
    builtCss,
    /--shimmer-track-height/,
    "built CSS is missing Elements theme styles",
  );
  for (const declarationPath of listFiles(
    join(packedPackageDir, "dist"),
  ).filter((filePath) => filePath.endsWith(".d.ts"))) {
    const declarationContent = readFileSync(declarationPath, "utf8");
    assert.doesNotMatch(
      declarationContent,
      /#elements\//,
      `${relative(packedPackageDir, declarationPath)} exposes a private package import`,
    );
    assertRelativeDeclarationImports(declarationPath, declarationContent);
  }

  const consumerDir = join(tempDir, "consumer");
  const packageScopeDir = join(consumerDir, "node_modules", "@gram-ai");
  mkdirSync(packageScopeDir, { recursive: true });
  symlinkSync(packedPackageDir, join(packageScopeDir, "elements"), "dir");
  writeFileSync(
    join(consumerDir, "package.json"),
    JSON.stringify({ private: true, type: "module" }),
  );

  const defaultResolutions = resolveExports(consumerDir);
  assertResolutions(defaultResolutions, "defaultTarget", packedPackageDir);

  writeFileSync(
    join(consumerDir, "contract.ts"),
    [
      'import type { ElementsConfig } from "@gram-ai/elements";',
      'import { createChatSession, type SessionHandlerOptions } from "@gram-ai/elements/server/core";',
      'const config: ElementsConfig = { projectSlug: "contract-test" };',
      'const options: SessionHandlerOptions = { embedOrigin: "https://example.com", userIdentifier: "contract-test" };',
      "void createChatSession;",
      "void config;",
      "void options;",
    ].join("\n"),
  );
  writeFileSync(
    join(consumerDir, "tsconfig.json"),
    JSON.stringify({
      compilerOptions: {
        module: "ESNext",
        moduleResolution: "Bundler",
        noEmit: true,
        skipLibCheck: true,
        strict: true,
        target: "ES2024",
      },
      files: ["contract.ts"],
    }),
  );
  execFileSync(resolve(workspaceDir, "node_modules/.bin/tsc"), ["-p", "."], {
    cwd: consumerDir,
    stdio: "pipe",
  });
  const defaultDeclarationFiles = execFileSync(
    resolve(workspaceDir, "node_modules/.bin/tsc"),
    ["-p", ".", "--listFilesOnly"],
    { cwd: consumerDir, encoding: "utf8" },
  );
  assert.match(
    defaultDeclarationFiles,
    /\/dist\/index\.d\.ts/,
    "default type resolution did not use dist/index.d.ts",
  );
  assert.match(
    defaultDeclarationFiles,
    /\/dist\/server\/core\.d\.ts/,
    "default type resolution did not use dist/server/core.d.ts",
  );
  assert.doesNotMatch(
    defaultDeclarationFiles,
    /\/unpacked\/package\/src\//,
    "default type resolution unexpectedly used package source",
  );

  execFileSync(
    process.execPath,
    [
      "--input-type=module",
      "--eval",
      'const module = await import("@gram-ai/elements/server/core"); if (typeof module.createChatSession !== "function") process.exit(1);',
    ],
    { cwd: consumerDir, stdio: "pipe" },
  );

  rmSync(join(packedPackageDir, "dist"), { recursive: true });
  const sourceResolutions = resolveExports(consumerDir, ["gram-source"]);
  assertResolutions(sourceResolutions, "source", packedPackageDir);

  console.log("Elements package contracts passed");
} finally {
  rmSync(tempDir, { recursive: true, force: true });
}

function resolveExports(consumerDir, conditions = []) {
  const script = `
    const specifiers = ${JSON.stringify(
      exportContracts.map(({ specifier }) => specifier),
    )};
    console.log(JSON.stringify(Object.fromEntries(
      specifiers.map((specifier) => [specifier, import.meta.resolve(specifier)]),
    )));
  `;
  const output = execFileSync(
    process.execPath,
    [
      ...conditions.map((condition) => `--conditions=${condition}`),
      "--input-type=module",
      "--eval",
      script,
    ],
    { cwd: consumerDir, encoding: "utf8" },
  );
  return JSON.parse(output);
}

function assertResolutions(resolutions, targetKind, packedPackageDir) {
  for (const contract of exportContracts) {
    const resolvedUrl = resolutions[contract.specifier];
    assert.ok(resolvedUrl, `did not resolve ${contract.specifier}`);
    const resolvedPath = realpathSync(fileURLToPath(resolvedUrl));
    const expectedPath = realpathSync(
      join(packedPackageDir, contract[targetKind]),
    );
    assert.equal(
      resolvedPath,
      expectedPath,
      `${contract.specifier} did not resolve to ${contract[targetKind]}`,
    );
  }
}

function listFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const entryPath = join(directory, entry.name);
    return entry.isDirectory() ? listFiles(entryPath) : entryPath;
  });
}

function assertRelativeDeclarationImports(declarationPath, content) {
  const code = content
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/^\s*\/\/.*$/gm, "");

  for (const match of code.matchAll(/(["'])(\.\.?\/[^"']+)\1/g)) {
    const specifier = match[2];
    const target = resolve(dirname(declarationPath), specifier);
    const withoutJavaScriptExtension = target.replace(/\.(?:c|m)?js$/, "");
    const candidates = [
      target,
      `${withoutJavaScriptExtension}.d.ts`,
      `${withoutJavaScriptExtension}.d.mts`,
      `${withoutJavaScriptExtension}.d.cts`,
      join(target, "index.d.ts"),
      join(target, "index.d.mts"),
      join(target, "index.d.cts"),
    ];
    assert.ok(
      candidates.some(
        (candidate) => existsSync(candidate) && statSync(candidate).isFile(),
      ),
      `${declarationPath} imports missing declaration ${specifier}`,
    );
  }
}
