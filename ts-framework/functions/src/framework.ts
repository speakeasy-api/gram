import * as z from "zod";
import * as zm from "zod/mini";

type Prettify<T> = {
  [K in keyof T]: T[K];
} & {};

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
  /**
   * The name of the tool.
   */
  name: TName;
  /**
   * A useful description of the tool that is presented to LLMs.
   */
  description?: string;
  /**
   * The input schema for the tool.
   */
  inputSchema: TInputSchema;
  /**
   * The function that implements the tool call.
   */
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

export type ManifestVariables = Record<
  string,
  { description?: string | undefined }
>;

export type ManifestTool = {
  name: string;
  description?: string;
  inputSchema: unknown;
  variables?: ManifestVariables;
  meta?: unknown;
};

export type ManifestResource = {
  name: string;
  title?: string | undefined;
  description?: string | undefined;
  uri: string;
  mimeType?: string | undefined;
  variables?: ManifestVariables;
  meta?: unknown;
};

export type Manifest = {
  version: string;
  tools?: ManifestTool[];
  resources?: ManifestResource[];
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
  /**
   * The parsed environment variables available to the tool.
   */
  readonly env: Env;
  /**
   * The abort signal for the tool execution. This can be passed down to fetch
   * calls and other async operations to propagate cancellation.
   */
  readonly signal: AbortSignal;
  constructor(signal: AbortSignal, env: Env) {
    this.signal = signal;
    this.env = env;
  }

  /**
   * Cause a function execution to fail with the given error message packaged
   * as a HTTP Response.
   */
  fail<V extends { error: string; stack?: never }>(
    data: V,
    options?: { status?: number },
  ): never {
    assert(false, data, options);
  }

  /**
   * Constructs a response with data serialized to JSON
   */
  json<V>(data: V): JSONResponse<V> {
    return new Response(JSON.stringify(data), {
      status: 200,
      headers: {
        "Content-Type": "application/json",
      },
    }) as JSONResponse<V>;
  }

  /**
   * Constructs a Markdown response
   */
  markdown<V extends string>(data: V): TextResponse<V> {
    return new Response(data, {
      status: 200,
      headers: {
        "Content-Type": "text/markdown;charset=UTF-8",
      },
    }) as TextResponse<V>;
  }

  /**
   * Constructs a plain text response
   */
  text<V extends string>(data: V): TextResponse<V> {
    return new Response(data, {
      status: 200,
      headers: {
        "Content-Type": "text/plain;charset=UTF-8",
      },
    }) as TextResponse<V>;
  }

  /**
   * Constructs an HTML response
   */
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
    /**
     * When set to true, runtime validation is disabled and tool input schemas
     * are only used to generate JSON Schema for tool listing.
     */
    lax?: boolean;
    /**
     * The environment variables to use when executing tools. If not provided,
     * `process.env` will be used. This is useful for testing and local
     * development.
     */
    env?: Record<string, string>;
    /**
     * The schema for environment variables that will be made available to
     * tools.
     */
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

  /**
   * Registers a tool with the Gram instance.
   */
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

  /**
   * Invokes a registered tool with a given input.
   */
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
        variables?: ManifestVariables;
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

function envMapFromJSONSchema(jsonSchema: unknown): ManifestVariables {
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

  const out: ManifestVariables = {};
  for (const [key, value] of Object.entries(parsed.properties)) {
    out[key] = {
      ...(value.description != null ? { description: value.description } : {}),
    };
  }

  return out;
}
