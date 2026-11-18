import fs from "node:fs";
import path from "node:path";
import type { Plugin } from "vite";

// External integrations reference these public URLs independently of the
// dashboard's hashed CSS assets. Keep both outputs from one font source.
export function stableFonts(): Plugin {
  const fonts = path.join(import.meta.dirname, "src/fonts");
  return {
    name: "stable-fonts",
    apply: "build",
    generateBundle() {
      for (const file of fs.readdirSync(fonts, { recursive: true })) {
        const relative = file.toString();
        if (!relative.endsWith(".woff2")) continue;
        this.emitFile({
          type: "asset",
          fileName: `fonts/${relative.split(path.sep).join("/")}`,
          source: fs.readFileSync(path.join(fonts, relative)),
        });
      }
    },
  };
}
