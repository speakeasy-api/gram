import { describe, expect, it } from "vitest";
import type { DetectionDescriptorResult } from "@gram/client/models/components";
import { buildCelEnv, checkExpr } from "./cel-env";

// A representative descriptor mirroring server celenv.Descriptor(): the opaque
// `field`, the `celenv.celTool` object, the field-typed variables + tools list,
// and the eight member overloads. buildCelEnv only reads names/types, so the
// human-facing fields (descriptions/signatures/macros) are omitted via cast.
const descriptor = {
  types: [
    { name: "field", opaque: true, fields: [] },
    {
      name: "celenv.celTool",
      opaque: false,
      fields: [
        { name: "name", type: "field", description: "" },
        { name: "server", type: "field", description: "" },
        { name: "function", type: "field", description: "" },
        { name: "args", type: "field", description: "" },
      ],
    },
  ],
  variables: [
    { name: "kind", type: "string", displayType: "string", description: "" },
    { name: "content", type: "field", displayType: "field", description: "" },
    { name: "prompt", type: "field", displayType: "field", description: "" },
    { name: "output", type: "field", displayType: "field", description: "" },
    {
      name: "tools",
      type: "list<celenv.celTool>",
      displayType: "list(tool)",
      description: "",
    },
  ],
  functions: [
    ...[
      "matchRegex",
      "matchText",
      "matchExact",
      "matchPrefix",
      "matchSuffix",
      "matchGlob",
    ].map((name) => ({
      name,
      overloadId: `field_${name}_string`,
      member: true,
      receiverType: "field",
      params: [{ name: "pattern", type: "string" }],
      returnType: "bool",
      signature: "",
      description: "",
    })),
    {
      name: "get",
      overloadId: "field_get_string",
      member: true,
      receiverType: "field",
      params: [{ name: "path", type: "string" }],
      returnType: "field",
      signature: "",
      description: "",
    },
    {
      name: "present",
      overloadId: "field_present",
      member: true,
      receiverType: "field",
      params: [],
      returnType: "bool",
      signature: "",
      description: "",
    },
  ],
  macros: [],
} as unknown as DetectionDescriptorResult;

const env = buildCelEnv(descriptor);

describe("cel-env checker", () => {
  it.each([
    "content.present()",
    'prompt.matchText("rm -rf")',
    'content.matchRegex("(?i)password")',
    'content.get("payload.sql").matchText("DROP")',
    'tools.exists(t, t.function.matchRegex("bash") && t.args.get("command").matchText("rm"))',
    "tools.all(t, t.name.present())",
    "tools.exists(t, has(t.args))",
    'kind == "user_message"',
  ])("accepts %s as a bool predicate", (expr) => {
    const r = checkExpr(env, expr);
    expect(r).toEqual({ valid: true, type: "bool" });
  });

  it.each([
    'nope.matchText("x")',
    "content.foo",
    'tools.exists(t, t.nope.matchRegex("x"))',
    "content.matchText(123)",
  ])("rejects %s", (expr) => {
    const r = checkExpr(env, expr);
    expect(r.valid).toBe(false);
    if (!r.valid) expect(r.message).toBeTruthy();
  });

  it("reports a non-bool result type (caller enforces bool)", () => {
    expect(checkExpr(env, 'content.get("a")')).toEqual({
      valid: true,
      type: "field",
    });
  });

  it("gives a source range for a bad reference", () => {
    const r = checkExpr(env, 'nope.matchText("x")');
    expect(r.valid).toBe(false);
    if (!r.valid) {
      expect(r.range).toBeDefined();
      expect(r.range!.end).toBeGreaterThan(r.range!.start);
    }
  });
});
