// @vitest-environment node
import { describe, expect, it } from "vitest";
import { extractFields } from "../inspect/toolSchemaFields";
import {
  codeModeCallSnippet,
  DEFAULT_CODE_MODE_SOURCE,
} from "./codeModeSnippet";

const fields = extractFields({
  type: "object",
  properties: {
    repoName: {
      type: "string",
      description: 'GitHub repository: owner/repo (e.g. "facebook/react")',
    },
    limit: { type: "integer" },
    labels: { type: "array", items: { type: "string" } },
    verbose: { type: "boolean" },
    mode: { const: "fast" },
    id: { type: "string" },
  },
  required: ["repoName", "id"],
});

it("names empty required arguments and leaves empty optional ones out", () => {
  expect(codeModeCallSnippet("deepwiki--read_wiki", fields, {})).toBe(
    [
      'result = await tools.call("deepwiki--read_wiki", {',
      '    "repoName": "<owner/repo>",',
      '    "mode": "fast",',
      '    "id": "<id>",',
      "})",
      'result["data"] if result["ok"] else result',
    ].join("\n"),
  );
});

it("writes typed values as Python literals, escaping strings", () => {
  expect(
    codeModeCallSnippet("deepwiki--read_wiki", fields, {
      repoName: 'a "quoted"\nname',
      id: "x",
      limit: "5",
      labels: '["bug", null]',
      verbose: "true",
    }),
  ).toBe(
    [
      'result = await tools.call("deepwiki--read_wiki", {',
      '    "repoName": "a \\"quoted\\"\\nname",',
      '    "limit": 5,',
      '    "labels": ["bug", None],',
      '    "verbose": True,',
      '    "mode": "fast",',
      '    "id": "x",',
      "})",
      'result["data"] if result["ok"] else result',
    ].join("\n"),
  );
});

describe("a composed root schema", () => {
  const composed = extractFields({
    anyOf: [
      { properties: { a: { type: "string" } } },
      { properties: { b: { type: "integer" } } },
    ],
  });

  it.each(["", "{not json", "[1]"])(
    "names the arguments when the editor holds %j",
    (text) => {
      expect(codeModeCallSnippet("a--b", composed, { arguments: text })).toBe(
        'result = await tools.call("a--b", "<arguments>")\nresult["data"] if result["ok"] else result',
      );
    },
  );

  it("writes each key of the arguments object", () => {
    expect(
      codeModeCallSnippet("a--b", composed, {
        arguments: '{"a": "x", "b": 2, "c": true}',
      }),
    ).toBe(
      [
        'result = await tools.call("a--b", {',
        '    "a": "x",',
        '    "b": 2,',
        '    "c": True,',
        "})",
        'result["data"] if result["ok"] else result',
      ].join("\n"),
    );
  });
});

it("calls a tool without arguments with an empty dict", () => {
  expect(codeModeCallSnippet("a--b", [], {})).toBe(
    'result = await tools.call("a--b", {})\nresult["data"] if result["ok"] else result',
  );
});

it("starts the editor with a program that lists the callable tools", () => {
  expect(DEFAULT_CODE_MODE_SOURCE).toContain("tools.search");
});
