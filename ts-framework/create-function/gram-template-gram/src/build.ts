import { buildFunctions } from "@gram-ai/functions/build";
import { join } from "path";

async function build() {
  await buildFunctions({
    outDir: "dist",
    entrypoint: join(import.meta.dirname, "functions.ts"),
    export: "default",
  });
}

if (import.meta.main) {
  await build();
}
