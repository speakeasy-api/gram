// @vitest-environment node
import { expect, it } from "vitest";
import { extractFields } from "../inspect/toolSchemaFields";
import { codeModeCallSnippet } from "./codeModeSnippet";

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

it("calls a tool without arguments with an empty dict", () => {
  expect(codeModeCallSnippet("a--b", [], {})).toBe(
    'result = await tools.call("a--b", {})\nresult["data"] if result["ok"] else result',
  );
});
