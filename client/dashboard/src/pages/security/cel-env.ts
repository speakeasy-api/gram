import { Environment } from "@marcbachmann/cel-js";
import type { DetectionDescriptorResult } from "@gram/client/models/components";
import type { CelSchema } from "./cel-monaco-editor";

export type CelCheck =
  | { valid: true; type: string }
  | { valid: false; message: string; range?: { start: number; end: number } };

const noop = () => undefined;

/** Build a cel-js type-checking environment from the backend descriptor — the
 *  same source the Go engine compiles, so the editor can't drift. Only types,
 *  variables, and function overloads are registered; CEL's macros
 *  (has/exists/all/map/...) are built into cel-js. Handlers are no-ops: we only
 *  type-check expressions in the browser, never evaluate them. */
export function buildCelEnv(
  descriptor: DetectionDescriptorResult,
): Environment {
  const env = new Environment();

  // Types first (variables and function receivers reference them). An opaque
  // type registers with no fields, so member access fails but its methods work.
  for (const t of descriptor.types) {
    const fields: Record<string, string> = {};
    for (const f of t.fields ?? []) fields[f.name] = f.type;
    env.registerType(t.name, { fields });
  }

  for (const v of descriptor.variables) {
    env.registerVariable(v.name, v.type);
  }

  for (const f of descriptor.functions) {
    const params = (f.params ?? []).map((p) => p.type).join(", ");
    const receiver = f.member ? `${f.receiverType}.` : "";
    env.registerFunction(
      `${receiver}${f.name}(${params}): ${f.returnType}`,
      noop,
    );
  }

  return env;
}

/** Project the descriptor into the Monaco completion schema: variables (with
 *  the member fields of object/list-of-object types), functions, and macros.
 *  The same descriptor drives validation, completion, and the reference panel,
 *  so all three stay in lockstep with the engine. */
export function descriptorToCelSchema(
  descriptor: DetectionDescriptorResult,
): CelSchema {
  const fieldsByType = new Map<
    string,
    { name: string; type: string; description: string }[]
  >();
  for (const t of descriptor.types) {
    if (!t.opaque) fieldsByType.set(t.name, t.fields ?? []);
  }
  const elementFields = (machine: string) => {
    const list = /^list<(.+)>$/.exec(machine);
    return fieldsByType.get(list?.[1] ?? machine) ?? [];
  };

  return {
    variables: descriptor.variables.map((v) => ({
      name: v.name,
      detail: v.displayType,
      doc: v.description,
      fields: elementFields(v.type).map((f) => ({
        name: f.name,
        detail: "field",
        doc: f.description,
        gettable: v.name === "tool_calls" && f.name === "args",
      })),
    })),
    functions: descriptor.functions.map((f) => ({
      name: f.name,
      detail: f.signature,
      doc: f.description,
    })),
    macros: descriptor.macros.map((m) => ({
      name: m.name,
      detail: m.signature,
      doc: m.description,
      // List macros (list.exists/...) complete after a dot; has() is global.
      member: m.signature.startsWith("list."),
    })),
  };
}

/** Type-check an expression against the env. On success returns the inferred
 *  type; on failure a message plus the source range that drives the editor's
 *  inline error marker. Never throws. */
export function checkExpr(env: Environment, expr: string): CelCheck {
  let result;
  try {
    result = env.check(expr);
  } catch (e) {
    return {
      valid: false,
      message: e instanceof Error ? e.message : String(e),
    };
  }
  if (result.valid) {
    return { valid: true, type: result.type ?? "dyn" };
  }
  const err = result.error;
  return {
    valid: false,
    message: err?.message ?? "Invalid expression",
    range: err?.range
      ? { start: err.range.start, end: err.range.end }
      : undefined,
  };
}
