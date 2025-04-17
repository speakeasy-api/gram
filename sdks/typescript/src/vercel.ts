import { GramAPICore } from "./core.js";
import { instancesGetBySlug } from "./funcs/instancesGetBySlug.js";
import { isBrowserLike } from "./lib/browsers.js";
import { SDK_METADATA } from "./lib/config.js";
import { GetInstanceResult } from "./models/components/getinstanceresult.js";
import { unwrapAsync } from "./types/fp.js";
import { jsonSchema, tool, ToolSet } from "ai";

type VercelAdapterConfig = {
  apiKey: string;
  project: string;
  toolset: string;
  environment?: string;
};

export class VercelAdapter {
  readonly #core: GramAPICore;
  readonly #config: VercelAdapterConfig;
  #instancePromise: Promise<GetInstanceResult>;

  constructor(config: VercelAdapterConfig) {
    this.#core = new GramAPICore();
    this.#config = config;
    this.#instancePromise = this.refetch();
  }

  async refetch(): Promise<GetInstanceResult> {
    this.#instancePromise = unwrapAsync(
      instancesGetBySlug(
        this.#core,
        {
          option2: {
            apikeyHeaderGramKey: this.#config.apiKey,
            projectSlugHeaderGramProject: this.#config.project,
          },
        },
        {
          toolsetSlug: this.#config.toolset,
          environmentSlug: this.#config.environment,
        }
      )
    );

    return this.#instancePromise;
  }

  async tools(): Promise<ToolSet> {
    const client = this.#core;
    const config = this.#config;
    const instance = await this.#instancePromise;

    const tools: ToolSet = {};

    for (const toolData of instance.tools) {
      tools[toolData.name] = tool({
        parameters: jsonSchema(
          toolData.schema ? JSON.parse(toolData.schema) : {}
        ),
        description: toolData.description,
        execute: async function callTool(args, { abortSignal }) {
          const security: Record<string, string> = {
            "gram-key": config.apiKey,
            "gram-project": config.project,
          };
          const headers: Record<string, string> = { ...security };

          if (isBrowserLike) {
            headers[
              "user-agent"
            ] = `@gram-ai/for/vercel ${SDK_METADATA.sdkVersion}`;
          }

          const retryConfig = {
            strategy: "backoff",
            retryConnectionErrors: true,
          } as const;

          const url = new URL(
            "http://localhost:8080/rpc/instances.invoke/tool"
          );
          url.searchParams.set("tool_id", toolData.id);
          if (config.environment) {
            url.searchParams.set("environment_slug", config.environment);
          }
          const request = new Request(url, {
            method: "POST",
            headers,
            body: JSON.stringify(args),
            signal: abortSignal || null,
          });

          const result = await client._do(request, {
            context: {
              baseURL: "http://localhost:8080",
              operationID: "invokeTool",
              oAuth2Scopes: null,
              retryConfig,
              resolvedSecurity: {
                headers: security,
                basic: {},
                queryParams: {},
                cookies: {},
                oauth2: { type: "none" },
              },
            },
            errorCodes: ["4XX", "5XX"],
            retryConfig,
            retryCodes: ["5XX"],
          });
          if (!result.ok) {
            throw new Error(`Tool call failed: ${toolData.name}`, {
              cause: result.error,
            });
          }

          const response = result.value;
          if (!response.ok) {
            const body = await response.text().catch(() => "");

            throw new Error(
              `Tool call failed: ${toolData.name}: ${response.statusText}: ${body}`
            );
          }

          return response.json();
        },
      });
    }

    return tools;
  }
}
