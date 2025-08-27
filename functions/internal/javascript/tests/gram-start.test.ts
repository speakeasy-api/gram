import { mkdtemp, writeFile, readFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { main } from "../gram-start.mjs";
import { test, expect } from "vitest";

async function fakepipe() {
  const dir = await mkdtemp(join(tmpdir(), "gramfunc-"));
  const responsePath = join(dir, "response.txt");
  await writeFile(responsePath, "");
  return responsePath;
}

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
