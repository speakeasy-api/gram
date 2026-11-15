// @vitest-environment node
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { build } from "vite";
import { expect, it } from "vitest";
import { stableFonts } from "../stable-fonts-plugin.ts";

it("wires stable fonts into the deployed build and serves them cross-origin", () => {
  const dashboard = path.resolve(import.meta.dirname, "..");
  expect(
    fs.readFileSync(path.join(dashboard, "vite.config.ts"), "utf8"),
  ).toContain("stableFonts(),");
  expect(fs.readFileSync(path.join(dashboard, "Dockerfile"), "utf8")).toContain(
    "COPY dist /usr/share/nginx/html",
  );
  const nginx = fs.readFileSync(path.join(dashboard, "nginx.conf"), "utf8");
  const fontLocation = nginx.match(/location \^~ \/fonts\/ \{([^}]+)\}/)?.[1];
  expect(fontLocation).toContain("try_files $uri =404;");
  expect(fontLocation).toContain("expires 1d;");
  expect(fontLocation).toContain(
    'add_header Access-Control-Allow-Origin "*" always;',
  );
  expect(fontLocation).toContain(
    'add_header Cross-Origin-Resource-Policy "cross-origin" always;',
  );
});

it("ships stable external font URLs alongside hashed dashboard assets", async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "dashboard-fonts-"));
  const fonts = path.resolve(import.meta.dirname, "fonts");
  const regular = "diatype/ABCDiatype-Regular.woff2";
  try {
    const input = path.join(root, "entry.js");
    fs.writeFileSync(
      input,
      `import font from ${JSON.stringify(path.join(fonts, regular) + "?url")}; console.log(font);`,
    );
    await build({
      configFile: false,
      root,
      logLevel: "silent",
      plugins: [stableFonts()],
      build: {
        assetsInlineLimit: 0,
        rolldownOptions: { input },
      },
    });
    for (const file of fs.readdirSync(fonts, { recursive: true })) {
      const relative = file.toString();
      if (!relative.endsWith(".woff2")) continue;
      expect(fs.readFileSync(path.join(root, "dist/fonts", relative))).toEqual(
        fs.readFileSync(path.join(fonts, relative)),
      );
    }
    expect(fs.readdirSync(path.join(root, "dist/assets"))).toEqual(
      expect.arrayContaining([
        expect.stringMatching(/^ABCDiatype-Regular-.+\.woff2$/),
      ]),
    );
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});
