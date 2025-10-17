import { mkdir, open, stat, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import esbuild from "esbuild";
import { Gram } from "../framework.ts";
import archiver from "archiver";

export async function buildFunctions(options: {
  entrypoint: string;
  export: string;
  outDir: string;
  cwd?: string;
}) {
  const cwd = options.cwd ?? process.cwd();
  const entrypoint = resolve(cwd, options.entrypoint);
  const gram = await import(entrypoint).then((mod) => {
    const exportsym = options.export ?? "gram";
    const gramExport = mod[exportsym];
    if (!(gramExport instanceof Gram)) {
      throw new Error(
        `Export "${exportsym}" does not appear to be an instance of Gram`,
      );
    }

    if (
      exportsym !== "default" &&
      typeof mod["handleToolCall"] !== "function"
    ) {
      throw new Error(
        `Either make the Gram instance the default export or export a "handleToolCall" function in ${entrypoint}.`,
      );
    }

    return gramExport;
  });

  const manifest = gram.manifest();

  await mkdir(options.outDir, { recursive: true });
  const outFile = resolve(options.outDir, "manifest.json");
  await writeFile(outFile, JSON.stringify(manifest, null, 2));

  await bundleFunction({
    entrypoint,
    outFile: resolve(options.outDir, "functions.js"),
  });

  const archive = archiver("zip", { zlib: { level: 9 } });
  const { promise, resolve: res, reject: rej } = Promise.withResolvers<void>();
  archive.on("error", rej);
  archive.on("close", res);

  const output = await open(resolve(options.outDir, "gram.zip"), "w");
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
