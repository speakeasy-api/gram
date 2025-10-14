import { writeFile, open, stat, mkdir } from "node:fs/promises";
import { join, resolve } from "node:path";
import esbuild from "esbuild";
import archiver from "archiver";

export type BuildMCPServerResult = {
  files: Array<{ path: string; size: number }>;
};

export async function buildMCPServer(options: {
  serverEntrypoint: string;
  serverExport: string;
  functionEntrypoint: string;
  outDir: string;
}): Promise<BuildMCPServerResult> {
  await mkdir(options.outDir, { recursive: true });

  const manifest = await buildFunctionsManifest({
    entrypoint: options.serverEntrypoint,
    serverExport: options.serverExport,
  });
  await writeFile(
    join(options.outDir, "manifest.json"),
    JSON.stringify(manifest, null, 2),
  );

  await bundleFunction({
    entrypoint: options.functionEntrypoint,
    outFile: join(options.outDir, "functions.js"),
  });

  const archive = archiver("zip", { zlib: { level: 9 } });
  const { promise, resolve, reject } = Promise.withResolvers<void>();
  archive.on("error", reject);
  archive.on("close", resolve);

  const output = await open(join(options.outDir, "gram.zip"), "w");
  archive.pipe(output.createWriteStream());
  archive.file(join(options.outDir, "manifest.json"), {
    name: "manifest.json",
  });
  archive.file(join(options.outDir, "functions.js"), { name: "functions.js" });
  await archive.finalize();
  await promise;
  await output.close();

  const zipstats = await stat(join(options.outDir, "gram.zip"));
  return {
    files: [{ path: join(options.outDir, "gram.zip"), size: zipstats.size }],
  };
}

async function buildFunctionsManifest(options: {
  entrypoint: string;
  serverExport?: string;
  cwd?: string;
}): Promise<{
  version: string;
  tools: Array<{
    name: string;
    description?: string | undefined;
    inputSchema: unknown;
  }>;
}> {
  const cwd = options.cwd ?? process.cwd();
  const entrypoint = resolve(cwd, options.entrypoint);

  const { InMemoryTransport } = await import(
    "@modelcontextprotocol/sdk/inMemory.js"
  );
  const { Client } = await import("@modelcontextprotocol/sdk/client/index.js");

  const server = await import(entrypoint).then((mod) => {
    const exportsym = options.serverExport ?? "server";
    const serverExport = mod[exportsym];
    const klass = serverExport?.constructor?.name;
    if (klass !== "McpServer") {
      throw new Error(
        `Export "${exportsym}" does not appear to be an instance of McpServer`,
      );
    }

    return serverExport;
  });

  const mcpClient = new Client({
    name: "@gram-ai/functions",
    version: "0.0.0",
  });
  const [clientTransport, serverTransport] =
    InMemoryTransport.createLinkedPair();
  await server.connect(serverTransport);
  await mcpClient.connect(clientTransport);

  const res = await mcpClient.listTools();
  const tools = res.tools.map((tool) => {
    return {
      name: tool.name,
      description: tool.description,
      inputSchema: tool.inputSchema,
    };
  });

  return { version: "0.0.0", tools };
}

async function bundleFunction(options: {
  entrypoint: string;
  outFile: string;
}): Promise<Array<{ path: string; hash: string }>> {
  const res = await esbuild.build({
    entryPoints: [options.entrypoint],
    outfile: options.outFile,
    bundle: true,
    treeShaking: true,
    minify: true,
    platform: "node",
    target: ["node22"],
    format: "esm",
  });

  return (
    res.outputFiles?.map((f) => ({
      path: f.path,
      hash: f.hash,
    })) || []
  );
}
