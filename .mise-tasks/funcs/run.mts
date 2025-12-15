#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Start a local Gram Functions runner for development and testing."

//USAGE flag "--code <file>" help="Path to file containing the function code to run."
//USAGE flag "--name <name>" default="gf-runner" required=#true help="A name for the runner container instance."
//USAGE flag "--image <image>" default="gram-runner-nodejs22:dev" required=#true help="The Docker image to use for the runner."

import path from "node:path";
import { $, chalk, fs } from "zx";
import { isCancel, confirm } from "@clack/prompts";

const SAMPLE_CODE = `
export async function handleToolCall(call) {
  const { name, input } = call;
  console.log("AAAAH", name)

  if (name !== "greet") {
    throw new Error(\`Unknown tool: \${name}\`);
  }
  return new Response(JSON.stringify({ message: \`Hello, \${input.user}!\` }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
`;

async function run() {
  const name = process.env["usage_name"] || "gf-runner";
  const image = process.env["usage_image"] || "gram-runner-nodejs22";

  const imageExists = await $`docker images -q ${image}`.quiet();
  if (!imageExists.stdout.trim()) {
    const ans = await confirm({
      message: `The image "${image}" does not exist. Build it now? (mise run build:functions-local)`,
    });
    if (isCancel(ans) || !ans) {
      console.log(
        "Aborted. You can build the image manually with: mise run build:functions-local.",
      );
      return;
    }

    console.log(chalk.bold(`Building image "${image}"...`));
    await $({ stdio: "inherit" })`mise run build:functions-local`;
  }

  const nameFilter = `name=${name}`;
  const ps = await $`docker ps -q --filter ${nameFilter}`;
  if (ps.stdout.trim()) {
    const ans = await confirm({
      message: `A container with the name "${name}" is already running. Kill it?`,
    });
    if (isCancel(ans) || !ans) {
      console.log("Aborted.");
      return;
    }

    await $`docker rm -f ${name}`;
  }

  let code = process.env["usage_code"] || null;
  const tmpDir = await fs.mkdtemp("/tmp/gram-runner-");
  if (!code) {
    const tmpFile = `${tmpDir}/functions.js`;
    await fs.writeFile(tmpFile, SAMPLE_CODE.trim());
    code = tmpFile;
  }

  // Zip the function code file
  const zipPath = path.join(tmpDir, `code.zip`);
  await $`zip -j ${zipPath} ${code}`;

  console.log(
    `Starting Gram Functions runner "${name}" using image "${image}"...`,
  );

  const env = [
    "--env",
    `GRAM_SERVER_URL=${process.env["GRAM_SERVER_URL"]}`,
    "--env",
    `GRAM_FUNCTION_AUTH_SECRET=${process.env["GRAM_ENCRYPTION_KEY"]}`,
    "--env",
    `GRAM_PROJECT_ID=${crypto.randomUUID()}`,
    "--env",
    `GRAM_DEPLOYMENT_ID=${crypto.randomUUID()}`,
    "--env",
    `GRAM_FUNCTION_ID=${crypto.randomUUID()}`,
  ];

  await $({
    stdio: "inherit",
  })`docker run -d --rm --name ${name} ${env} -p 8888:8888 -v ${zipPath}:/data/code.zip ${image}`;

  console.log(
    chalk.bold("Example tool call:\n\n") +
      chalk.blueBright(
        `
mise run funcs:tool \\
  --name greet \\
  --input '{"user": "world"}'
\n`.slice(1, -1),
      ),
  );
}

await run();
