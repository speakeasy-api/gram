import type * as z from "zod";
import * as zm from "zod/mini";

type Prettify<T> = {
  [K in keyof T]: T[K];
} & {};

type VarDef = { description?: string };

export class ResponseError extends Error {
  constructor(message?: string, options?: ErrorOptions) {
    super(message, options);
    this.name = "ResponseError";
  }
}

export type ToolDefinition<
  TName extends string,
  TInputSchema extends z.core.$ZodShape,
  Vars extends string,
  Result extends Response,
> = {
  name: TName;
  description?: string;
  inputSchema: TInputSchema;
  variables?: Record<string, VarDef>;
  execute: (
    ctx: ToolContext<Record<Vars, string | undefined>>,
    input: z.infer<z.ZodObject<TInputSchema>>,
  ) => Promise<Result>;
};

export type ToolSignature<T> =
  T extends ToolDefinition<
    infer Name,
    infer InputSchema,
    infer Vars,
    infer Result
  >
    ? [Name, z.infer<z.ZodObject<InputSchema>>, Vars, Result]
    : never;

type InferInput<T> = ToolSignature<T>[1];

type InferResult<T> = ToolSignature<T>[3];

export type Manifest = {
  version: string;
  tools: Array<{
    name: string;
    description?: string;
    inputSchema: unknown;
    variables?: Record<string, VarDef>;
  }>;
};

export function assert<V extends { error: string; stack?: never }>(
  cond: boolean,
  data: V,
  options?: { status?: number },
): asserts cond {
  if (!cond) {
    throw new Response(
      JSON.stringify({ ...data, stack: new ResponseError().stack }),
      {
        status: options?.status || 500,
        headers: {
          "Content-Type": "application/json",
        },
      },
    );
  }
}

class ToolContext<
  Vars extends Record<string, string | undefined> = Record<
    string,
    string | undefined
  >,
> {
  #keys: Set<string>;
  #env: Record<string, string | undefined>;
  readonly signal: AbortSignal;
  constructor(
    signal: AbortSignal,
    vardef: Record<keyof Vars, VarDef>,
    env?: Record<string, string | undefined>,
  ) {
    this.signal = signal;
    this.#keys = new Set(Object.keys(vardef));
    this.#env = env || {};
  }

  get vars(): Vars {
    const result: Record<string, string> = {};
    for (const key of this.#keys) {
      if (Object.hasOwn(this.#env, key) && this.#env[key] != null) {
        result[key] = this.#env[key] || "";
      }
    }
    return result as Vars;
  }

  fail<V extends { error: string; stack?: never }>(
    data: V,
    options?: { status?: number },
  ): never {
    assert(false, data, options);
  }

  json<V>(data: V): JSONResponse<V> {
    return new Response(JSON.stringify(data), {
      status: 200,
      headers: {
        "Content-Type": "application/json",
      },
    }) as JSONResponse<V>;
  }
  text<V extends string>(data: V): TextResponse<V> {
    return new Response(data, {
      status: 200,
      headers: {
        "Content-Type": "text/plain;charset=UTF-8",
      },
    }) as TextResponse<V>;
  }
  html(data: string): TextResponse<string> {
    return new Response(data, {
      status: 200,
      headers: {
        "Content-Type": "text/html",
      },
    }) as TextResponse<string>;
  }
}

export interface JSONResponse<T> extends Response {
  json(): Promise<T>;
}

export interface TextResponse<T extends string> extends Response {
  text(): Promise<T>;
}

export class Gram<
  TTools extends {
    [k: string]: ToolDefinition<any, any, string, Response>;
  } = {},
> {
  private tools: Map<string, ToolDefinition<any, any, string, Response>>;
  private lax: boolean;
  private env: Record<string, string | undefined> | undefined;

  constructor(opts?: { lax?: boolean; env?: Record<string, string> }) {
    this.tools = new Map();
    this.lax = Boolean(opts?.lax);
    this.env = opts?.env;
  }

  tool<
    TName extends string,
    TInputSchema extends z.core.$ZodShape,
    TVariables extends string,
    Res extends Response,
  >(
    definition: ToolDefinition<TName, TInputSchema, TVariables, Res>,
  ): Gram<
    Prettify<
      TTools & {
        [k in TName]: ToolDefinition<TName, TInputSchema, TVariables, Res>;
      }
    >
  > {
    this.tools.set(definition.name, definition as any);
    return this as any;
  }

  async handleToolCall<TName extends keyof TTools & string>(
    request: {
      name: TName;
      input: InferInput<TTools[TName]>;
    },
    options?: { signal?: AbortSignal },
  ): Promise<InferResult<TTools[TName]>> {
    const tool = this.tools.get(request.name);
    if (!tool) {
      throw new Error(`Tool not found: ${request.name}`);
    }

    const ctx = new ToolContext(
      options?.signal || new AbortController().signal,
      tool.variables || {},
      this.env || process.env,
    );

    const schema = zm.object(tool.inputSchema);
    const vres = schema.safeParse(request.input);
    let validatedInput: Record<string, unknown> = {};
    if (vres.success) {
      validatedInput = vres.data;
    } else if (
      this.lax &&
      typeof request.input === "object" &&
      request.input !== null
    ) {
      validatedInput = request.input as Record<string, unknown>;
    } else {
      ctx.fail(
        { error: vres.error.message, issues: vres.error.issues },
        { status: 400 },
      );
    }

    return (await tool.execute(ctx, validatedInput)) as InferResult<
      TTools[TName]
    >;
  }

  manifest(): Manifest {
    const tools = Array.from(this.tools.values()).map((tool) => {
      const schema = zm.object(tool.inputSchema);
      const inputSchema = zm.toJSONSchema(schema);
      const result: {
        name: string;
        description?: string;
        inputSchema: unknown;
        variables?: Record<string, VarDef>;
      } = {
        name: tool.name,
        inputSchema: inputSchema,
      };
      if (tool.description != null) {
        result.description = tool.description;
      }
      if (tool.variables != null && Object.keys(tool.variables).length > 0) {
        result.variables = tool.variables;
      }
      return result;
    });

    return {
      version: "0.0.0",
      tools,
    };
  }
}
