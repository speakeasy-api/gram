import { existsSync, createWriteStream } from "node:fs";
import path from "node:path";
import url from "node:url";

/**
 * @param {string[]} args
 * @returns {{pipePath: string, toolCall: {name: string, input: unknown}}}
 */
function parseArgs(args) {
  args = args.slice(2);

  if (args.length !== 2) {
    throw new Error(
      "Expected two command-line argument but got " + args.length
    );
  }

  const pipePath = args[0];
  if (typeof pipePath !== "string" || !existsSync(pipePath)) {
    throw new Error(`Named pipe does not exist: ${pipePath}`);
  }

  const callArg = args[1];
  if (typeof callArg !== "string") {
    throw new Error(
      `Invalid tool call argument type: expected string, got ${typeof callArg}`
    );
  }

  const toolCall = JSON.parse(callArg);

  if (typeof toolCall !== "object" || toolCall === null) {
    throw new Error("Tool call argument must be a valid JSON object");
  }

  if (typeof toolCall.name !== "string") {
    throw new Error("Argument must have a string 'name' property");
  }

  return { pipePath, toolCall };
}

/**
 * @param {(name: string, input: unknown) => Promise<Response>} func
 * @param {string} name
 * @param {unknown} input
 * @returns {Promise<Response>}
 */
async function callTool(func, name, input) {
  try {
    const response = await func(name, input);
    if (!(response instanceof Response)) {
      throw new Error("Tool call did not return a Response");
    }

    return response;
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e);

    return new Response(JSON.stringify({ error: msg }), {
      status: 500,
      headers: {
        "Content-Type": "application/json",
        "Content-Length": String(Buffer.byteLength(msg, "utf8")),
      },
    });
  }
}

/**
 * @param {import("node:fs").WriteStream} pipeStream
 * @param {Response} response
 */
async function writeHTTPResponse(pipeStream, response) {
  const status = response.status || 200;
  const statusText = response.statusText || "";

  pipeStream.write(`HTTP/1.1 ${status} ${statusText}\r\n`);

  response.headers.forEach((value, key) => {
    pipeStream.write(`${key}: ${value}\r\n`);
  });

  pipeStream.write("\r\n");

  if (!response.body) {
    pipeStream.end();
    return;
  }

  /**
   * @type {ReadableStreamDefaultReader<Uint8Array<ArrayBuffer>> | null}
   */
  let reader = null;
  try {
    reader = response.body.getReader();
    while (true) {
      const { done, value } = await reader.read();
      if (value != null) pipeStream.write(value);
      if (done) break;
    }
  } finally {
    pipeStream.end();
    reader?.releaseLock();
  }
}

const USER_CODE_PATH = path.join(process.cwd(), "functions.js");

export async function main(args = process.argv, codePath = USER_CODE_PATH) {
  const { handleToolCall: func } = await import(codePath);
  if (typeof func !== "function") {
    const filename = path.basename(codePath);
    throw new Error(`handleToolCall in ${filename} is not a function`);
  }

  const { pipePath, toolCall } = parseArgs(args);

  const pipeStream = createWriteStream(pipePath, { flush: true });
  const response = await callTool(func, toolCall.name, toolCall.input);
  await writeHTTPResponse(pipeStream, response);
}

if (import.meta.url.startsWith("file:")) {
  const modulePath = url.fileURLToPath(import.meta.url);
  if (process.argv[1] === modulePath) {
    main();
  }
}
