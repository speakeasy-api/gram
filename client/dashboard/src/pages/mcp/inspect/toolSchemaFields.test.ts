// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  extractFields,
  optionKey,
  toArguments,
  type ToolField,
} from "./toolSchemaFields";

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
        options: [
          { label: "low", value: "low" },
          { label: "high", value: "high" },
        ],
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

  it("reads a composed root schema as one arguments editor", () => {
    const fields = extractFields({
      anyOf: [
        { properties: { id: { type: "string" } }, required: ["id"] },
        { properties: { slug: { type: "string" } }, required: ["slug"] },
      ],
    });
    expect(fields).toMatchObject([{ kind: "arguments", required: true }]);
    expect(toArguments(fields, { arguments: '{"id":"a"}' })).toEqual({
      ok: true,
      args: { id: "a" },
    });
    expect(toArguments(fields, { arguments: "[1]" })).toEqual({
      ok: false,
      error: "Arguments must be a JSON object.",
    });
    expect(
      extractFields({
        type: "object",
        additionalProperties: { type: "string" },
      }),
    ).toMatchObject([{ kind: "arguments" }]);
  });

  it("offers number enums and const unions as typed choices", () => {
    const [level, mode] = extractFields({
      properties: {
        level: { type: "integer", enum: [1, 2] },
        mode: {
          oneOf: [
            { const: "a", title: "Mode A" },
            { const: 3, title: "Three" },
          ],
        },
      },
    });
    expect(level).toMatchObject({
      kind: "enum",
      options: [
        { label: "1", value: 1 },
        { label: "2", value: 2 },
      ],
    });
    expect(mode).toMatchObject({
      kind: "enum",
      options: [
        { label: "Mode A", value: "a" },
        { label: "Three", value: 3 },
      ],
    });
    if (!level || !mode) throw new Error("no fields");
    expect(
      toArguments([level, mode], {
        level: optionKey(2),
        mode: optionKey(3),
      }),
    ).toEqual({ ok: true, args: { level: 2, mode: 3 } });
  });

  it("reads a nullable anyOf as its non-null type", () => {
    const [count, tags] = extractFields({
      properties: {
        count: {
          description: "How many",
          anyOf: [{ type: "integer" }, { type: "null" }],
        },
        tags: {
          anyOf: [
            { type: "array", items: { type: "string" } },
            { type: "null" },
          ],
        },
      },
    });
    expect(count).toMatchObject({
      kind: "number",
      integer: true,
      typeLabel: "integer",
      description: "How many",
    });
    expect(tags).toMatchObject({ kind: "json", typeLabel: "string[]" });
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

  it("rejects a non-finite number and a fractional integer", () => {
    expect(toArguments([field("n", "number")], { n: "Infinity" })).toEqual({
      ok: false,
      error: "n must be a number.",
    });
    const integer: ToolField = {
      name: "n",
      kind: "number",
      integer: true,
      required: false,
      typeLabel: "integer",
    };
    expect(toArguments([integer], { n: "1.5" })).toEqual({
      ok: false,
      error: "n must be a whole number.",
    });
    expect(toArguments([integer], { n: "3" })).toEqual({
      ok: true,
      args: { n: 3 },
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

  it("keeps null as a choice in a nullable enum", () => {
    const [color] = extractFields({
      properties: { color: { enum: ["red", null] } },
      required: ["color"],
    });
    expect(color).toMatchObject({
      kind: "enum",
      options: [
        { label: "red", value: "red" },
        { label: "null", value: null },
      ],
    });
    if (!color) throw new Error("no field");
    expect(toArguments([color], { color: optionKey(null) })).toEqual({
      ok: true,
      args: { color: null },
    });
  });

  it("requires arguments when a composed editor is left blank", () => {
    const fields = extractFields({ oneOf: [{ type: "object" }] });
    expect(toArguments(fields, { arguments: "  " })).toEqual({
      ok: false,
      error: "Arguments are required.",
    });
  });

  it.each([
    ["allOf", { allOf: [{ properties: { a: { type: "string" } } }] }],
    [
      "oneOf",
      {
        oneOf: [
          { properties: { a: { type: "string" } } },
          { properties: { b: { type: "string" } } },
        ],
      },
    ],
    ["$ref", { $ref: "#/$defs/Args", $defs: { Args: { type: "object" } } }],
    [
      "patternProperties",
      { type: "object", patternProperties: { "^x-": { type: "string" } } },
    ],
  ])("reads a root %s as one arguments editor", (_name, schema) => {
    expect(extractFields(schema)).toMatchObject([
      { name: "arguments", kind: "arguments", required: true, schema },
    ]);
  });
});
