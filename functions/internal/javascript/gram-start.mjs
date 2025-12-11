import { existsSync } from "node:fs";
import { open } from "node:fs/promises";
import path from "node:path";
import process from "node:process";
import { Writable } from "node:stream";
import url from "node:url";

export const ERROR_CODES = /** @type {const} */ ({
  UNEXPECTED: "gram_err_000",
  INVALID_TOOL_RESULT: "gram_err_001",
  TOOL_CALL_FAILED: "gram_err_002",
  IMPORT_FAILURE: "gram_err_003",
  INVALID_TOOL_FUNC: "gram_err_004",
  INVALID_RESOURCE_RESULT: "gram_err_005",
  RESOURCE_REQUEST_FAILED: "gram_err_006",
  INVALID_RESOURCE_FUNC: "gram_err_007",
});

class FunctionsError extends Error {
  /**
   * @param {typeof ERROR_CODES[keyof typeof ERROR_CODES]} code
   * @param {string} message
   * @param {unknown} [cause]
   */
  constructor(code, message, cause) {
    super(`${message} (${code})`, { cause });
    this.name = "FunctionsError";
    this.code = code;
  }

  toJSON() {
    /** @type {unknown} */
    let cause = undefined;
    if (this.cause instanceof Error) {
      cause = {
        name: this.cause.name,
        message: this.cause.message,
        stack: this.cause.stack,
      };
    } else if (this.cause != null) {
      cause = {
        message: String(this.cause),
      };
    } else {
      cause = undefined;
    }

    return {
      name: this.name,
      message: this.message,
      cause,
    };
  }
}

/**
 * @param {string[]} args
 * @returns {{
 *   type: "tool",
 *   pipePath: string,
 *   request: { name: string, input: unknown }
 * } | {
 *   type: "resource",
 *   pipePath: string,
 *   request: { uri: string, input: unknown}
 * }}}
 */
function parseArgs(args) {
  args = args.slice(2);

  if (args.length < 2 || args.length > 3) {
    throw new Error(
      "Expected two or three command-line arguments but got " + args.length,
    );
  }

  const pipePath = args[0];
  if (typeof pipePath !== "string" || !existsSync(pipePath)) {
    throw new Error(`Named pipe does not exist: ${pipePath}`);
  }

  const requestArg = args[1];
  if (typeof requestArg !== "string") {
    throw new Error(
      `Invalid request argument type: expected string, got ${typeof requestArg}`,
    );
  }

  // Default to "tool" for backward compatibility
  const typeArg = args[2] || "tool";
  if (typeArg !== "tool" && typeArg !== "resource") {
    throw new Error(
      `Invalid type argument: expected "tool" or "resource", got "${typeArg}"`,
    );
  }

  const request = JSON.parse(requestArg);

  if (typeof request !== "object" || request === null) {
    throw new Error("Request argument must be a valid JSON object");
  }

  // Validate the request has the correct property based on type
  if (typeArg === "tool" && typeof request.name !== "string") {
    throw new Error("Tool request must have a string 'name' property");
  }

  if (typeArg === "resource" && typeof request.uri !== "string") {
    throw new Error("Resource request must have a string 'uri' property");
  }

  return { pipePath, request, type: typeArg };
}

/**
 * @param {(call: {name: string, input: unknown}) => Promise<Response>} func
 * @param {string} name
 * @param {unknown} input
 * @returns {Promise<Response>}
 */
async function callTool(func, name, input) {
  try {
    const response = await func({ name, input });
    if (!(response instanceof Response)) {
      throw new FunctionsError(
        ERROR_CODES.INVALID_TOOL_RESULT,
        "Tool call did not return a valid response",
        `Expected instance of \`Response\` but got \`${typeof response}\``,
      );
    }
    return response;
  } catch (e) {
    if (e instanceof FunctionsError) {
      throw e;
    } else if (e instanceof Response) {
      return e;
    } else {
      throw new FunctionsError(
        ERROR_CODES.TOOL_CALL_FAILED,
        "Tool call failed",
        e,
      );
    }
  }
}

/**
 * @param {(call: {uri: string, input: unknown}) => Promise<Response>} func
 * @param {string} uri
 * @param {unknown} input
 * @returns {Promise<Response>}
 */
async function callResource(func, uri, input) {
  try {
    const response = await func({ uri, input });
    if (!(response instanceof Response)) {
      throw new FunctionsError(
        ERROR_CODES.INVALID_RESOURCE_RESULT,
        "Resource request did not return a valid response",
        `Expected instance of \`Response\` but got \`${typeof response}\``,
      );
    }
    return response;
  } catch (e) {
    if (e instanceof FunctionsError) {
      throw e;
    } else if (e instanceof Response) {
      return e;
    } else {
      throw new FunctionsError(
        ERROR_CODES.RESOURCE_REQUEST_FAILED,
        "Resource request failed",
        e,
      );
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
    }),
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

  const dest = pipeFile.createWriteStream();

  await response.body.pipeTo(Writable.toWeb(dest));
}

/**
 * @param {string} codePath
 * @returns {Promise<{ok: true, value: (call: {name: string, input: unknown}) => Promise<Response>} | {ok: false, error: FunctionsError}>}
 */
async function importToolCallHandler(codePath) {
  try {
    const mod = await import(codePath).catch((e) => {
      const filename = path.basename(codePath);
      throw new FunctionsError(
        ERROR_CODES.IMPORT_FAILURE,
        `Unable to import user code: ${filename}`,
        e,
      );
    });

    let f = mod["handleToolCall"];
    if (typeof f !== "function") {
      const def = await mod["default"];
      // Bind `f` to `def` so if `f` contains references to `this`, they will
      // continue to work correctly.
      f =
        typeof def?.handleToolCall === "function"
          ? def.handleToolCall.bind(def)
          : undefined;
    }

    if (typeof f !== "function") {
      const filename = path.basename(codePath);

      throw new FunctionsError(
        ERROR_CODES.INVALID_TOOL_FUNC,
        "Unable to call tool",
        "handleToolCall function not found in " + filename,
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
          e,
        ),
      };
    }
  }
}

/**
 * @param {string} codePath
 * @returns {Promise<{ok: true, value: (call: {uri: string, input: unknown}) => Promise<Response>} | {ok: false, error: FunctionsError}>}
 */
async function importResourceHandler(codePath) {
  try {
    const mod = await import(codePath).catch((e) => {
      const filename = path.basename(codePath);
      throw new FunctionsError(
        ERROR_CODES.IMPORT_FAILURE,
        `Unable to import user code: ${filename}`,
        e,
      );
    });

    let f = mod["handleResources"];
    if (typeof f !== "function") {
      const def = await mod["default"];
      // Bind `f` to `def` so if `f` contains references to `this`, they will
      // continue to work correctly.
      f =
        typeof def?.handleResources === "function"
          ? def.handleResources.bind(def)
          : undefined;
    }

    if (typeof f !== "function") {
      const filename = path.basename(codePath);

      throw new FunctionsError(
        ERROR_CODES.INVALID_RESOURCE_FUNC,
        "Unable to handle resources",
        "handleResources function not found in " + filename,
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
          e,
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
      toolCall.input,
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

/**
 *
 * @param {import("node:fs/promises").FileHandle} pipeFile
 * @param {{uri: string, input: unknown}} resourceRequest
 * @param {string} codePath
 * @returns
 */
async function handleResources(pipeFile, resourceRequest, codePath) {
  const importResult = await importResourceHandler(codePath);
  if (!importResult.ok) {
    await writeFunctionsError(pipeFile, importResult.error);
    return;
  }

  try {
    const res = await callResource(
      importResult.value,
      resourceRequest.uri,
      resourceRequest.input,
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
  const { pipePath, request, type } = parseArgs(args);

  const pipeFile = await open(pipePath, "w");
  try {
    switch (type) {
      case "tool":
        await handleToolCall(
          pipeFile,
          /** @type {{name: string, input: unknown}} */ (request),
          codePath,
        );
        break;
      case "resource":
        await handleResources(
          pipeFile,
          /** @type {{uri: string, input: unknown}} */ (request),
          codePath,
        );
        break;
      default:
        throw new Error(`Unrecognized type: ${type}`);
    }
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
