import { writeFile, open, stat, mkdir } from "node:fs/promises";
import { join, resolve } from "node:path";
import esbuild from "esbuild";
import archiver from "archiver";
import type { Client } from "@modelcontextprotocol/sdk/client";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { Client as MCPClient } from "@modelcontextprotocol/sdk/client/index.js";
import {
  McpError,
  ErrorCode as McpErrorCode,
} from "@modelcontextprotocol/sdk/types.js";

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
    _meta?: unknown;
  }>;
  resources?: Array<{
    name: string;
    description?: string | undefined;
    uri: string;
    mimeType?: string | undefined;
    _meta?: unknown;
  }>;
}> {
  const cwd = options.cwd ?? process.cwd();
  const entrypoint = resolve(cwd, options.entrypoint);

  const server = await import(entrypoint).then((mod) => {
    const exportsym = options.serverExport ?? "server";
    const serverExport = mod[exportsym];
    if (typeof serverExport === "undefined") {
      throw new Error(
        `Export "${exportsym}" in entrypoint "${entrypoint}" is undefined`,
      );
    }

    const klass = serverExport?.constructor?.name;
    if (klass !== "McpServer" && klass !== "Server") {
      throw new Error(
        `Export "${exportsym}" does not appear to be an instance of McpServer`,
      );
    }

    return serverExport;
  });

  const mcpClient = new MCPClient({
    name: "@gram-ai/functions",
    version: "0.0.0",
  });
  const [clientTransport, serverTransport] =
    InMemoryTransport.createLinkedPair();
  await server.connect(serverTransport);
  await mcpClient.connect(clientTransport);

  let tools = await collectTools(mcpClient);
  let resources = await collectResources(mcpClient);

  return { version: "0.0.0", tools, resources };
}

async function collectTools(client: Client) {
  try {
    const res = await client.listTools();
    return res.tools.map((tool) => {
      return {
        name: tool.name,
        description: tool.description,
        inputSchema: tool.inputSchema,
        meta: {
          "gram.ai/kind": "mcp-passthrough",
          ...tool._meta,
        },
      };
    });
  } catch (err) {
    if (err instanceof McpError && err.code === McpErrorCode.MethodNotFound) {
      console.warn("No tools registered");
    } else {
      throw err;
    }
    return [];
  }
}

async function collectResources(client: Client) {
  try {
    const resourcesResponse = await client.listResources();
    return resourcesResponse.resources.map((resource) => {
      return {
        name: resource.name,
        description: resource.description,
        uri: resource.uri,
        mimeType: resource.mimeType,
        title: resource.title,
        meta: {
          "gram.ai/kind": "mcp-passthrough",
          ...resource._meta,
        },
      };
    });
  } catch (err) {
    if (err instanceof McpError && err.code === McpErrorCode.MethodNotFound) {
      console.warn("No tools registered");
    } else {
      throw err;
    }
  }

  return [];
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
