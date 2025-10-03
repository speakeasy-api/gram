import { mkdtemp, open, readFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { test, expect, beforeAll, afterEach, afterAll } from "vitest";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { ERROR_CODES, main } from "../gram-start.mjs";

async function fakepipe() {
  const dir = await mkdtemp(join(tmpdir(), "gramfunc-"));
  const responsePath = join(dir, "response.txt");
  const handle = await open(responsePath, "w");
  await handle.close();
  return responsePath;
}

const server = setupServer(
  ...[
    http.get("http://localhost:7357/pet", () => {
      return HttpResponse.json({ name: "Finn", type: "cat" });
    }),
    http.get("http://localhost:7357/logo", async () => {
      const logo = await readFile(join(import.meta.dirname, "logo.png"));
      return HttpResponse.arrayBuffer(logo.buffer, {
        headers: { "Content-Type": "image/png" },
      });
    }),
  ]
);

beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

test("valid tool call with args", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    name: "get-weather",
    input: { city: "San Francisco" },
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args],
    join(import.meta.dirname, "good.js")
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toMatchSnapshot();
});

test("valid tool call without args", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    name: "ping",
    input: {},
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args],
    join(import.meta.dirname, "good.js")
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toMatchSnapshot();
});

test("valid tool call adds custom headers", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    name: "list-products",
    input: { cursor: "1" },
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args],
    join(import.meta.dirname, "good.js")
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toMatchSnapshot();

  expect(content).toMatch(/^x-next-cursor: 4$/im);
});

test("valid tool call explicit 4XX", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    name: "create-charge",
    input: { charge: -100 },
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args],
    join(import.meta.dirname, "good.js")
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toMatchSnapshot();
});

test("valid tool call explicit 4XX", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    name: "create-charge",
    input: { charge: -100 },
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args],
    join(import.meta.dirname, "good.js")
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toMatchSnapshot();
});

test("proxy good downstream fetch calls", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    name: "proxy",
    input: { url: "http://localhost:7357/pet" },
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args],
    join(import.meta.dirname, "good.js")
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toMatchSnapshot();

  const pet = JSON.parse(content.trim().split("\n").at(-1) ?? "");
  expect(pet).toEqual({ name: "Finn", type: "cat" });
});

test("proxy good downstream that returns binary data", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    name: "proxy",
    input: { url: "http://localhost:7357/logo" },
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args],
    join(import.meta.dirname, "good.js")
  );

  const text = await readFile(pipePath, "utf-8");

  // Get the content length of the body and then we'll be able to load the
  // result file again as a byte array and read the image data.
  const lhdr = text
    .trim()
    .split("\n")
    .find((line) => line.toLowerCase().startsWith("content-length:"));
  const lenstr = lhdr?.split(":")?.[1]?.trim() ?? "0";
  const len = parseInt(lenstr, 10);

  const binary = await readFile(pipePath);
  const imageData = binary.subarray(-len);
  const logo = await readFile(join(import.meta.dirname, "logo.png"));
  expect(imageData).toEqual(logo);
});

test("catches tool calls that throw", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    name: "fail-tool",
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args],
    join(import.meta.dirname, "good.js")
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toContain(ERROR_CODES.TOOL_CALL_FAILED);
  expect(content).toMatchSnapshot();

  const res = JSON.parse(content.trim().split("\n").at(-1) ?? "");
  expect(res).toEqual({
    name: "FunctionsError",
    message: expect.stringMatching(/Intentional failure/),
  });
});

test("fails when tool call does not return Response", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    name: "null-tool",
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args],
    join(import.meta.dirname, "good.js")
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toContain(ERROR_CODES.INVALID_TOOL_RESULT);
  expect(content).toMatchSnapshot();
});

test("fails when functions file does not exist", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    name: "null-tool",
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args],
    join(import.meta.dirname, "nonexistent.js")
  );

  const content = await readFile(pipePath, "utf-8");
  const res = JSON.parse(content.trim().split("\n").at(-1) ?? "");
  expect(res).toEqual({
    name: "FunctionsError",
    message: expect.stringMatching(
      /Unable to import user code \(gram_err_003\)/
    ),
    cause: expect.stringMatching(/^Failed to import nonexistent\.js/),
  });
});

test("fails when functions file does not export handleToolCall", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    name: "null-tool",
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args],
    join(import.meta.dirname, "empty.js")
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toContain(ERROR_CODES.INVALID_TOOL_FUNC);
  expect(content).toMatchSnapshot();
});
