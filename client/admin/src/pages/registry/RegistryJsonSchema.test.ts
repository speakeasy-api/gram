import { expect, it, vi } from "vitest";
import canonical from "../../../../../server/internal/mcpregistry/contract/record.schema.json";
import { registryJsonDiagnostics } from "./RegistryJsonSchema";
// @ts-expect-error Monaco ships no declarations for its worker implementation.
import { JSONWorker } from "monaco-editor/languages/features/json/jsonWorker.js";

const uri = "gram-registry://draft/test.json";
function worker(text: string, modelUri = uri) {
  return new JSONWorker(
    {
      getMirrorModels: () => [
        { uri: { toString: () => modelUri }, version: 1, getValue: () => text },
      ],
    },
    {
      languageSettings: registryJsonDiagnostics,
      languageId: "json",
      enableSchemaRequest: false,
    },
  ) as {
    doComplete: (
      uri: string,
      position: { line: number; character: number },
    ) => Promise<{ items: { label: string }[] }>;
    doValidation: (uri: string) => Promise<{ message: string }[]>;
  };
}

it("registers the exact canonical offline schema only for registry models", () => {
  expect(registryJsonDiagnostics.schemas?.[0]?.schema).toBe(canonical);
  expect(canonical.$schema).toBe(
    "https://json-schema.org/draft/2020-12/schema",
  );
  expect(registryJsonDiagnostics.schemas?.[0]?.fileMatch).toEqual([
    "gram-registry://draft/*.json",
  ]);
  expect(registryJsonDiagnostics.enableSchemaRequest).toBe(false);
  expect(registryJsonDiagnostics.schemaValidation).toBe("error");
});
it("offers canonical server properties and remote transport values in the real JSON worker", async () => {
  const properties = '{"server": { }}';
  const propertyResult = await worker(properties).doComplete(uri, {
    line: 0,
    character: 12,
  });
  expect(propertyResult.items.map((item) => item.label)).toEqual(
    expect.arrayContaining(["name", "version", "remotes"]),
  );
  const values = '{"server":{"remotes":[{"type": ""}]}}';
  const valueResult = await worker(values).doComplete(uri, {
    line: 0,
    character: values.indexOf('""') + 1,
  });
  expect(valueResult.items.map((item) => item.label)).toEqual(
    expect.arrayContaining(['"streamable-http"', '"sse"']),
  );
});
it("reports required fields without rejecting open extension metadata", async () => {
  expect(
    (await worker('{"server":{}}').doValidation(uri))
      .map((issue) => issue.message)
      .join(" "),
  ).toContain("name");
  const text =
    '{"server":{"name":"io.example/test","description":"Test","version":"1"},"extension":{"n":9007199254740993}}';
  expect(await worker(text).doValidation(uri)).toEqual([]);
});

it("resolves only bundled local references and does not fetch document schemas", async () => {
  for (const match of JSON.stringify(canonical).matchAll(
    /"\$ref":"#\/\$defs\/([^"/]+)"/g,
  )) {
    expect(canonical.$defs).toHaveProperty(match[1]!);
  }
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  try {
    await worker(
      '{"$schema":"https://example.com/never-fetch.json"}',
    ).doValidation(uri);
    expect(fetch).not.toHaveBeenCalled();
    const otherUri = "inmemory://other.json";
    expect(await worker("{}", otherUri).doValidation(otherUri)).toEqual([]);
  } finally {
    vi.unstubAllGlobals();
  }
});
