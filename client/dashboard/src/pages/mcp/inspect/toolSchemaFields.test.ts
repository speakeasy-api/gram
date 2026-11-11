// @vitest-environment node
import { describe, expect, it } from "vitest";
import { extractFields, toArguments, type ToolField } from "./toolSchemaFields";

describe("extractFields", () => {
  it("maps each property type to a field kind", () => {
    const fields = extractFields({
      type: "object",
      properties: {
        title: { type: "string", description: "Issue title" },
        estimate: { type: "number" },
        count: { type: "integer" },
        draft: { type: "boolean" },
        priority: { type: "string", enum: ["low", "high"] },
        labels: { type: "array", items: { type: "string" } },
        meta: { type: "object" },
        note: { type: ["string", "null"] },
      },
      required: ["title", "count"],
    });

    expect(fields).toMatchObject([
      {
        name: "title",
        required: true,
        description: "Issue title",
        kind: "string",
        typeLabel: "string",
      },
      {
        name: "estimate",
        required: false,
        description: undefined,
        kind: "number",
        typeLabel: "number",
      },
      {
        name: "count",
        required: true,
        description: undefined,
        kind: "number",
        typeLabel: "integer",
      },
      {
        name: "draft",
        required: false,
        description: undefined,
        kind: "boolean",
        typeLabel: "boolean",
      },
      {
        name: "priority",
        required: false,
        description: undefined,
        kind: "enum",
        options: ["low", "high"],
        typeLabel: "enum",
      },
      {
        name: "labels",
        required: false,
        description: undefined,
        kind: "json",
        typeLabel: "string[]",
      },
      {
        name: "meta",
        required: false,
        description: undefined,
        kind: "json",
        typeLabel: "object",
      },
      {
        name: "note",
        required: false,
        description: undefined,
        kind: "string",
        typeLabel: "string",
      },
    ]);
  });

  it("reads a string-or-list union as text that also takes JSON", () => {
    const [field] = extractFields({
      properties: {
        repoName: {
          anyOf: [
            { type: "string" },
            { type: "array", items: { type: "string" } },
          ],
        },
      },
    });
    expect(field).toMatchObject({
      kind: "text-or-json",
      typeLabel: "string | string[]",
      example: '["…"]',
    });
    if (!field) throw new Error("no field");
    expect(toArguments([field], { repoName: "owner/repo" })).toEqual({
      ok: true,
      args: { repoName: "owner/repo" },
    });
    expect(toArguments([field], { repoName: '["a/b","c/d"]' })).toEqual({
      ok: true,
      args: { repoName: ["a/b", "c/d"] },
    });
  });

  it("reads a const property as a fixed value", () => {
    const fields = extractFields({
      properties: { action: { const: "create", type: "string" } },
      required: ["action"],
    });
    expect(fields).toMatchObject([
      {
        name: "action",
        required: true,
        description: undefined,
        kind: "const",
        value: "create",
        typeLabel: "const",
      },
    ]);
  });

  it("unwraps a { jsonSchema } wrapper", () => {
    const fields = extractFields({
      jsonSchema: { properties: { q: { type: "string" } } },
    });
    expect(fields.map((f) => f.name)).toEqual(["q"]);
  });

  it("returns no fields for a missing schema", () => {
    expect(extractFields(undefined)).toEqual([]);
    expect(extractFields({ type: "object" })).toEqual([]);
  });
});

describe("toArguments", () => {
  const field = (
    name: string,
    kind: "string" | "number" | "boolean" | "json",
    required = false,
  ): ToolField => ({ name, kind, required, typeLabel: kind });

  it("rejects a missing required field", () => {
    expect(toArguments([field("title", "string", true)], {})).toEqual({
      ok: false,
      error: "title is required.",
    });
  });

  it("rejects a number that doesn't parse", () => {
    expect(toArguments([field("n", "number")], { n: "abc" })).toEqual({
      ok: false,
      error: "n must be a number.",
    });
  });

  it("rejects JSON that doesn't parse", () => {
    expect(toArguments([field("meta", "json")], { meta: "{oops" })).toEqual({
      ok: false,
      error: "meta must be valid JSON.",
    });
  });

  it("leaves empty optional fields out", () => {
    expect(
      toArguments(
        [field("title", "string"), field("n", "number"), field("b", "boolean")],
        { title: "  ", n: "" },
      ),
    ).toEqual({ ok: true, args: {} });
  });

  it("always sends a const value, whatever the form holds", () => {
    const constField: ToolField = {
      name: "action",
      required: true,
      kind: "const",
      value: "create",
      typeLabel: "const",
    };
    expect(toArguments([constField], {})).toEqual({
      ok: true,
      args: { action: "create" },
    });
    expect(toArguments([constField], { action: "other" })).toEqual({
      ok: true,
      args: { action: "create" },
    });
  });

  it("converts each kind to its value", () => {
    expect(
      toArguments(
        [
          field("title", "string", true),
          field("n", "number"),
          field("on", "boolean"),
          field("off", "boolean", true),
          field("meta", "json"),
        ],
        {
          title: "Hello",
          n: "42",
          on: "true",
          off: "",
          meta: '{"a":[1]}',
        },
      ),
    ).toEqual({
      ok: true,
      args: { title: "Hello", n: 42, on: true, off: false, meta: { a: [1] } },
    });
  });
});
