import { buildMCPServer } from "@gram-ai/functions/build";

async function build() {
  await buildMCPServer({
    outDir: "dist",
    functionEntrypoint: "./src/functions.ts",
    serverExport: "server",
    serverEntrypoint: "./src/mcp.ts",
  });
}

if (import.meta.url === `file://${process.argv[1]}`) {
  build();
}
