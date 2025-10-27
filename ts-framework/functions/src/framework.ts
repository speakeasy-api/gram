import * as z from "zod";
import * as zm from "zod/mini";

type Prettify<T> = {
  [K in keyof T]: T[K];
} & {};

type VarDef = { description?: string | undefined };

export class ResponseError extends Error {
  constructor(message?: string, options?: ErrorOptions) {
    super(message, options);
    this.name = "ResponseError";
  }
}

export type ToolDefinition<
  TName extends string,
  TInputSchema extends z.core.$ZodShape,
  Env,
  Result extends Response,
> = {
  name: TName;
  description?: string;
  inputSchema: TInputSchema;
  execute: (
    ctx: ToolContext<Env>,
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

class ToolContext<Env> {
  readonly env: Env;
  readonly signal: AbortSignal;
  constructor(signal: AbortSignal, env: Env) {
    this.signal = signal;
    this.env = env;
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

export type InferEnv<V extends z.core.$ZodShape> = z.core.$InferObjectOutput<
  V,
  Record<string, string | undefined>
>;

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
  EnvSchema extends z.core.$ZodShape = {
    readonly [x: string]: z.core.$ZodOptional<z.core.$ZodString>;
  },
> {
  #tools: Map<string, ToolDefinition<any, any, InferEnv<EnvSchema>, Response>>;
  #lax: boolean;
  #inputEnv?: Record<string, string | undefined> | undefined;
  #envSchema: EnvSchema;
  #envMemo!: InferEnv<EnvSchema>;

  constructor(opts?: {
    lax?: boolean;
    env?: Record<string, string>;
    envSchema?: EnvSchema;
  }) {
    this.#tools = new Map();
    this.#lax = Boolean(opts?.lax);
    this.#inputEnv = opts?.env;
    this.#envSchema = opts?.envSchema as EnvSchema;
  }

  get #env() {
    if (this.#envMemo == null) {
      const schema = this.#envSchema ? z.object(this.#envSchema) : z.unknown();
      this.#envMemo = schema.parse(
        this.#inputEnv ?? process.env,
      ) as InferEnv<EnvSchema>;
    }
    return this.#envMemo;
  }

  tool<
    TName extends string,
    TInputSchema extends z.core.$ZodShape,
    Res extends Response,
  >(
    definition: ToolDefinition<TName, TInputSchema, InferEnv<EnvSchema>, Res>,
  ): Gram<
    Prettify<
      TTools & {
        [k in TName]: ToolDefinition<
          TName,
          TInputSchema,
          InferEnv<EnvSchema>,
          Res
        >;
      }
    >,
    EnvSchema
  > {
    this.#tools.set(definition.name, definition as any);
    return this as any;
  }

  async handleToolCall<TName extends keyof TTools & string>(
    request: {
      name: TName;
      input: InferInput<TTools[TName]>;
    },
    options?: { signal?: AbortSignal },
  ): Promise<InferResult<TTools[TName]>> {
    const tool = this.#tools.get(request.name);
    if (!tool) {
      throw new Error(`Tool not found: ${request.name}`);
    }

    const ctx = new ToolContext(
      options?.signal || new AbortController().signal,
      this.#env,
    );

    const schema = zm.object(tool.inputSchema);
    const vres = schema.safeParse(request.input);
    let validatedInput: Record<string, unknown> = {};
    if (vres.success) {
      validatedInput = vres.data;
    } else if (
      this.#lax &&
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
    const tools = Array.from(this.#tools.values()).map((tool) => {
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
      if (this.#envSchema != null) {
        const obj = zm.object(this.#envSchema);
        result.variables = envMapFromJSONSchema(zm.toJSONSchema(obj));
      }

      return result;
    });

    return {
      version: "0.0.0",
      tools,
    };
  }
}

function envMapFromJSONSchema(jsonSchema: unknown): Record<string, VarDef> {
  const parsed = zm
    .object({
      properties: zm.record(
        zm.string(),
        zm.object({
          description: zm.optional(zm.string()),
        }),
      ),
    })
    .parse(jsonSchema);

  const out: Record<string, VarDef> = {};
  for (const [key, value] of Object.entries(parsed.properties)) {
    out[key] = {
      ...(value.description != null ? { description: value.description } : {}),
    };
  }

  return out;
}
