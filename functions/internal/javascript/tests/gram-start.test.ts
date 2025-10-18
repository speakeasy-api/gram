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
  ],
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
    ["node", "./gram-start.mjs", pipePath, args, "tool"],
    join(import.meta.dirname, "good.js"),
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toMatchSnapshot();
});

test("valid tool call from default export", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    name: "greet",
    input: { user: "Jane" },
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args, "tool"],
    join(import.meta.dirname, "good-default-export.js"),
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toMatchSnapshot();

  const res = JSON.parse(content.trim().split("\n").at(-1) ?? "");
  expect(res).toEqual({
    message: "Hello, Jane!",
  });
});

test("valid tool call without args", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    name: "ping",
    input: {},
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args, "tool"],
    join(import.meta.dirname, "good.js"),
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
    ["node", "./gram-start.mjs", pipePath, args, "tool"],
    join(import.meta.dirname, "good.js"),
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
    ["node", "./gram-start.mjs", pipePath, args, "tool"],
    join(import.meta.dirname, "good.js"),
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
    ["node", "./gram-start.mjs", pipePath, args, "tool"],
    join(import.meta.dirname, "good.js"),
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
    ["node", "./gram-start.mjs", pipePath, args, "tool"],
    join(import.meta.dirname, "good.js"),
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
    ["node", "./gram-start.mjs", pipePath, args, "tool"],
    join(import.meta.dirname, "good.js"),
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
    ["node", "./gram-start.mjs", pipePath, args, "tool"],
    join(import.meta.dirname, "good.js"),
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
    ["node", "./gram-start.mjs", pipePath, args, "tool"],
    join(import.meta.dirname, "good.js"),
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
    ["node", "./gram-start.mjs", pipePath, args, "tool"],
    join(import.meta.dirname, "nonexistent.js"),
  );

  const content = await readFile(pipePath, "utf-8");
  const res = JSON.parse(content.trim().split("\n").at(-1) ?? "");
  expect(res).toEqual({
    name: "FunctionsError",
    message: expect.stringMatching(
      /Unable to import user code \(gram_err_003\)/,
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
    ["node", "./gram-start.mjs", pipePath, args, "tool"],
    join(import.meta.dirname, "empty.js"),
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toContain(ERROR_CODES.INVALID_TOOL_FUNC);
  expect(content).toMatchSnapshot();
});

test("backward compatibility - tool call without type parameter", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    name: "ping",
    input: {},
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args],
    join(import.meta.dirname, "good.js"),
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toMatchSnapshot();
});

test("valid resource request with uri", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    uri: "file:///config.json",
    input: {},
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args, "resource"],
    join(import.meta.dirname, "goodResources.js"),
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toMatchSnapshot();

  const res = JSON.parse(content.trim().split("\n").at(-1) ?? "");
  expect(res).toEqual({
    version: "1.0.0",
    environment: "production",
  });
});

test("valid resource request with input", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    uri: "file:///data/users.csv",
    input: { limit: 3 },
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args, "resource"],
    join(import.meta.dirname, "goodResources.js"),
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toMatchSnapshot();
  expect(content).toContain("user1,user1@example.com");
  expect(content).toContain("user3,user3@example.com");
});

test("resource request with template rendering", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    uri: "file:///templates/email.html",
    input: { name: "Alice" },
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args, "resource"],
    join(import.meta.dirname, "goodResources.js"),
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toContain("Hello, Alice!");
  expect(content).toMatchSnapshot();
});

test("catches resource requests that throw", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    uri: "error://fail",
    input: {},
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args, "resource"],
    join(import.meta.dirname, "goodResources.js"),
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toContain(ERROR_CODES.RESOURCE_REQUEST_FAILED);
  expect(content).toMatchSnapshot();

  const res = JSON.parse(content.trim().split("\n").at(-1) ?? "");
  expect(res).toEqual({
    name: "FunctionsError",
    message: expect.stringMatching(/Resource access failed/),
  });
});

test("fails when resource request does not return Response", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    uri: "null://resource",
    input: {},
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args, "resource"],
    join(import.meta.dirname, "goodResources.js"),
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toContain(ERROR_CODES.INVALID_RESOURCE_RESULT);
  expect(content).toMatchSnapshot();
});

test("fails when functions file does not export handleResources", async () => {
  const pipePath = await fakepipe();
  const args = JSON.stringify({
    uri: "file:///config.json",
    input: {},
  });

  await main(
    ["node", "./gram-start.mjs", pipePath, args, "resource"],
    join(import.meta.dirname, "empty.js"),
  );

  const content = await readFile(pipePath, "utf-8");
  expect(content).toContain(ERROR_CODES.INVALID_RESOURCE_FUNC);
  expect(content).toMatchSnapshot();
});
