import { existsSync } from "node:fs";
import { open } from "node:fs/promises";
import process from "node:process";
import path from "node:path";
import url from "node:url";
import { Writable } from "node:stream";

export const ERROR_CODES = /** @type {const} */ ({
  UNEXPECTED: "gram_err_000",
  INVALID_TOOL_RESULT: "gram_err_001",
  TOOL_CALL_FAILED: "gram_err_002",
  IMPORT_FAILURE: "gram_err_003",
  INVALID_TOOL_FUNC: "gram_err_004",
});

class FunctionsError extends Error {
  /**
   * @param {typeof ERROR_CODES[keyof typeof ERROR_CODES]} code
   * @param {string} message
   * @param {string | undefined} [cause]
   */
  constructor(code, message, cause) {
    super(`${message} (${code})`);
    this.name = "FunctionsError";
    this.cause = cause;
    this.code = code;
  }

  toJSON() {
    return {
      name: this.name,
      message: this.message,
      cause: this.cause,
    };
  }
}

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
      throw new FunctionsError(
        ERROR_CODES.INVALID_TOOL_RESULT,
        "Tool call did not return a valid response",
        `Expected instance of \`Response\` but got \`${typeof response}\``
      );
    }
    return response;
  } catch (e) {
    if (e instanceof FunctionsError) {
      throw e;
    } else {
      let msg = e instanceof Error ? e.message : String(e);
      msg = msg || "Tool call failed";
      throw new FunctionsError(ERROR_CODES.TOOL_CALL_FAILED, msg);
    }
  }
}

/**
 *
 * @param {import("node:fs/promises").FileHandle} pipeFile
 * @param {FunctionsError} error
 */
async function writeFunctionsError(pipeFile, error) {
  const text = JSON.stringify(error.toJSON());

  return writeHTTPResponse(
    pipeFile,
    new Response(text, {
      status: 500,
      headers: {
        "Content-Type": "application/json",
        "Content-Length": String(Buffer.byteLength(text, "utf8")),
        "Gram-Functions-Error": error.code,
      },
    })
  );
}

/**
 * @param {import("node:fs/promises").FileHandle} pipeFile
 * @param {Response} response
 */
async function writeHTTPResponse(pipeFile, response) {
  const status = response.status || 200;
  const statusText = response.statusText || "";

  await pipeFile.write(`HTTP/1.1 ${status} ${statusText}\r\n`);

  for (const [key, value] of response.headers) {
    await pipeFile.write(`${key}: ${value}\r\n`);
  }

  await pipeFile.write("\r\n");

  if (!response.body) {
    return;
  }

  const dest = pipeFile.createWriteStream({
    autoClose: true,
    emitClose: true,
    flush: true,
  });

  await response.body.pipeTo(Writable.toWeb(dest));
}

/**
 * @param {string} codePath
 * @returns {Promise<{ok: true, value: (name: string, input: unknown) => Promise<Response>} | {ok: false, error: FunctionsError}>}
 */
async function importToolCallHandler(codePath) {
  try {
    const { handleToolCall: f } = await import(codePath).catch((e) => {
      const filename = path.basename(codePath);
      throw new FunctionsError(
        ERROR_CODES.IMPORT_FAILURE,
        "Unable to import user code",
        `Failed to import ${filename}: ${
          e instanceof Error ? e.message : String(e)
        }`
      );
    });

    if (typeof f !== "function") {
      const filename = path.basename(codePath);

      throw new FunctionsError(
        ERROR_CODES.INVALID_TOOL_FUNC,
        "Unable to call tool",
        "handleToolCall function not found in " + filename
      );
    }

    return { ok: true, value: f };
  } catch (e) {
    if (e instanceof FunctionsError) {
      return { ok: false, error: e };
    } else {
      return {
        ok: false,
        error: new FunctionsError(
          ERROR_CODES.UNEXPECTED,
          "Unexpected error occurred",
          e instanceof Error ? e.message : String(e)
        ),
      };
    }
  }
}

const USER_CODE_PATH = path.join(process.cwd(), "functions.js");

/**
 *
 * @param {import("node:fs/promises").FileHandle} pipeFile
 * @param {{name: string, input: unknown}} toolCall
 * @param {string} codePath
 * @returns
 */
async function handleToolCall(pipeFile, toolCall, codePath) {
  const importResult = await importToolCallHandler(codePath);
  if (!importResult.ok) {
    await writeFunctionsError(pipeFile, importResult.error);
    return;
  }

  try {
    const res = await callTool(
      importResult.value,
      toolCall.name,
      toolCall.input
    );
    await writeHTTPResponse(pipeFile, res);
  } catch (e) {
    if (e instanceof FunctionsError) {
      return await writeFunctionsError(pipeFile, e);
    } else {
      throw e;
    }
  }
}

export async function main(args = process.argv, codePath = USER_CODE_PATH) {
  const { pipePath, toolCall } = parseArgs(args);

  const pipeFile = await open(pipePath, "w");
  try {
    await handleToolCall(pipeFile, toolCall, codePath);
  } finally {
    await pipeFile.close();
  }
}

if (import.meta.url.startsWith("file:")) {
  const modulePath = url.fileURLToPath(import.meta.url);
  if (process.argv[1] === modulePath) {
    main();
  }
}
