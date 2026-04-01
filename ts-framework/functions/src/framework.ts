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
   * Optional annotations describing tool behavior hints (aligned with MCP spec).
   */
  annotations?: ToolAnnotations;
  /**
   * Optional metadata for the tool. Use `"ui/resourceUri"` to link a tool
   * to a UI resource (MCP Apps / SEP-1865).
   */
  meta?: Record<string, unknown>;
  /**
   * The function that implements the tool call.
   */
  execute: (
    ctx: ToolContext<Env>,
    input: z.infer<z.ZodObject<TInputSchema>>,
  ) => Promise<Result>;
};

type ToolConfig<
  TName extends string,
  TInputSchema extends z.core.$ZodShape,
  Env,
  Result extends Response,
> = ToolDefinition<TName, TInputSchema, Env, Result> & {
  lax: boolean;
  inputEnv?: Record<string, string | undefined>;
  envSchema?: z.core.$ZodShape;
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

export type ToolAnnotations = {
  /**
   * A human-readable title for the tool.
   */
  title?: string;
  readOnlyHint?: boolean;
  destructiveHint?: boolean;
  idempotentHint?: boolean;
  openWorldHint?: boolean;
};

export type ManifestVariables = Record<string, { description?: string }>;

export type ManifestTool = {
  name: string;
  description?: string;
  inputSchema: unknown;
  annotations?: ToolAnnotations;
  variables?: ManifestVariables;
  authInput?: {
    type: "oauth2";
    variable: string;
    gramEmail?: boolean;
  };
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

export type ResourceEntry = {
  name: string;
  uri: string;
  description: string;
  mimeType?: string;
  title?: string;
  content: string | (() => string | Promise<string>);
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
    readonly [x: string]:
      | z.core.$ZodString
      | z.core.$ZodOptional<z.core.$ZodString>;
  },
> {
  #tools: Map<string, ToolConfig<string, z.core.$ZodShape, any, Response>>;
  #resources: Map<string, ResourceEntry>;
  #lax: boolean;
  #inputEnv?: Record<string, string | undefined> | undefined;
  #envSchema?: EnvSchema;
  #authInput?: {
    type: "oauth2";
    variable: string;
    gramEmail?: boolean;
  };

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
    /**
     * Authentication configuration for OAuth2 tokens.
     */
    authInput?: {
      /**
       * The name of the environment variable that contains the OAuth2 access token.
       * Must be a key in envSchema.
       */
      oauthVariable: keyof EnvSchema & string;
      /**
       * When true, the authenticated Gram user's email will be available to
       * the function as the GRAM_USER_EMAIL environment variable.
       */
      gramEmail?: boolean;
    };
  }) {
    this.#tools = new Map();
    this.#resources = new Map();
    this.#lax = Boolean(opts?.lax);
    this.#inputEnv = opts?.env;
    this.#envSchema = opts?.envSchema;
    this.#authInput = opts?.authInput
      ? {
          type: "oauth2",
          variable: opts.authInput.oauthVariable,
          gramEmail: opts.authInput.gramEmail,
        }
      : undefined;
  }

  protected get tools() {
    return this.#tools;
  }

  protected get envSchema() {
    return this.#envSchema;
  }

  protected get lax() {
    return this.#lax;
  }

  protected get inputEnv() {
    return this.#inputEnv;
  }

  protected get resources() {
    return this.#resources;
  }

  /**
   * Registers a resource with the Gram instance.
   */
  resource(def: ResourceEntry): this {
    this.#resources.set(def.uri, def);
    return this;
  }

  /**
   * Registers a UI resource (MCP Apps / SEP-1865) with
   * `text/html;profile=mcp-app` MIME type.
   *
   * Accepts either raw `content` (full HTML string) or `body` + optional `styles`
   * which are wrapped in an HTML scaffold with a `Gram.onData(cb)` helper injected.
   *
   * If `uri` is omitted, it defaults to `ui://{name}`.
   *
   * @example
   * ```ts
   * gram.uiResource({
   *   name: "bar-chart",
   *   description: "Interactive bar chart",
   *   title: "Bar Chart",
   *   styles: `.chart { display: flex; align-items: end; gap: 4px; }`,
   *   body: `
   *     <div id="chart"></div>
   *     <script>
   *       Gram.onData((data) => {
   *         document.getElementById("chart").textContent = JSON.stringify(data);
   *       });
   *     </script>
   *   `,
   * });
   * ```
   */
  uiResource(
    def: {
      name: string;
      description: string;
      title?: string;
      uri?: string;
    } & (
      | {
          /** Raw HTML content for the resource. */
          content: string | (() => string | Promise<string>);
        }
      | {
          /** HTML body content — will be wrapped in a scaffold with Gram.onData() helper. */
          body: string;
          /** CSS styles injected into the scaffold's <style> tag. */
          styles?: string;
        }
    ),
  ): this {
    const uri = def.uri ?? `ui://${def.name}`;
    const content =
      "body" in def ? buildUIScaffold(def.body, def.styles) : def.content;
    return this.resource({
      name: def.name,
      uri,
      description: def.description,
      title: def.title,
      mimeType: "text/html;profile=mcp-app",
      content,
    });
  }

  /**
   * Reads a registered resource by URI and returns its content as a Response.
   */
  async handleResourceRead(request: { uri: string }): Promise<Response> {
    const resource = this.#resources.get(request.uri);
    if (!resource) {
      throw new Error(`Resource not found: ${request.uri}`);
    }
    const content =
      typeof resource.content === "function"
        ? await resource.content()
        : resource.content;
    return new Response(content, {
      status: 200,
      headers: {
        "Content-Type": resource.mimeType || "text/plain",
      },
    });
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
    this.#tools.set(definition.name, {
      ...definition,
      lax: this.#lax,
      inputEnv: this.#inputEnv,
      envSchema: this.envSchema,
    } as any);
    return this;
  }

  /**
   * Extends this Gram instance with another Gram instance's tools and environment schema.
   * Similar to Hono's route groups. Returns a new Gram instance with merged
   * tools and environment schemas.
   */
  extend<
    OtherTools extends {
      [k: string]: ToolDefinition<any, any, any, Response>;
    },
    OtherEnvSchema extends z.core.$ZodShape,
  >(
    other: Gram<OtherTools, OtherEnvSchema>,
  ): Gram<Prettify<TTools & OtherTools>, Prettify<EnvSchema & OtherEnvSchema>> {
    for (const [name, tool] of other.tools) {
      this.tools.set(name, tool);
    }
    for (const [uri, resource] of other.resources) {
      this.resources.set(uri, resource);
    }

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

    const envSchema = tool.envSchema ? z.object(tool.envSchema) : z.unknown();

    const ctx = new ToolContext(
      options?.signal || new AbortController().signal,
      envSchema.parse(tool.inputEnv ?? process.env),
    );

    const schema = zm.object(tool.inputSchema);
    const vres = schema.safeParse(request.input);
    let validatedInput: Record<string, unknown> = {};
    if (vres.success) {
      validatedInput = vres.data;
    } else if (
      tool.lax &&
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

      // Create a custom metadata registry to ensure descriptions are preserved
      const constructRegistryWithDescriptions = (schema: z.core.$ZodShape) => {
        const registry = new (zm as any).core.$ZodRegistry();
        Object.entries(schema).forEach(([_, zodSchema]) => {
          const description = (zodSchema as any).description;
          if (description) {
            registry.add(zodSchema, { description });
          }
        });
        return registry;
      };

      const registry = constructRegistryWithDescriptions(tool.inputSchema);
      const inputSchema = zm.toJSONSchema(schema, { metadata: registry });

      const result: {
        name: string;
        description?: string;
        inputSchema: unknown;
        annotations?: ToolAnnotations;
        variables?: ManifestVariables;
        authInput?: {
          type: "oauth2";
          variable: string;
          gramEmail?: boolean;
        };
        meta?: Record<string, unknown>;
      } = {
        name: tool.name,
        inputSchema: inputSchema,
      };
      if (tool.description != null) {
        result.description = tool.description;
      }

      if (tool.annotations != null) {
        result.annotations = tool.annotations;
      }

      if (tool.meta != null) {
        result.meta = tool.meta;
      }

      if (tool.envSchema != null) {
        const registry = constructRegistryWithDescriptions(tool.envSchema);

        const obj = z.object(tool.envSchema);
        result.variables = envMapFromJSONSchema(
          z.toJSONSchema(obj, { metadata: registry }),
        );
      }

      if (this.#authInput != null) {
        result.authInput = this.#authInput;
      }

      return result;
    });

    const resources: ManifestResource[] = Array.from(
      this.#resources.values(),
    ).map((r) => {
      const entry: ManifestResource = {
        name: r.name,
        uri: r.uri,
        description: r.description,
      };
      if (r.mimeType != null) {
        entry.mimeType = r.mimeType;
      }
      if (r.title != null) {
        entry.title = r.title;
      }
      return entry;
    });

    return {
      version: "0.0.0",
      tools,
      ...(resources.length > 0 ? { resources } : {}),
    };
  }
}

/**
 * Wraps body HTML and optional styles in a full HTML document with the
 * Gram.onData() communication helper injected.
 */
function buildUIScaffold(body: string, styles?: string): string {
  return `<!DOCTYPE html>
<html>
<head>
<style>
body{margin:0;padding:16px;font-family:system-ui,sans-serif}
${styles ?? ""}
</style>
</head>
<body>
${body}
<script>
window.Gram={onData(cb){window.addEventListener("message",e=>{if(e.data!=null)cb(e.data)})}};
</script>
</body>
</html>`;
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
