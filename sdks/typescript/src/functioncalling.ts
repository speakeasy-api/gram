import { GramAPICore } from "./core.js";
import { getServerUrlByKey } from "./environments.js";
import { instancesGetBySlug } from "./funcs/instancesGetBySlug.js";
import { isBrowserLike } from "./lib/browsers.js";
import { SDK_METADATA } from "./lib/config.js";
import { GetInstanceResult } from "./models/components/getinstanceresult.js";
import { unwrapAsync } from "./types/fp.js";

export type GramInstanceRequest = {
  project: string;
  toolset: string;
  environment?: string;
};

export type FunctionCallingTool = {
  name: string;
  description: string;
  parameters: object;
  execute: (input: string) => Promise<string>;
};

export class FunctionCallingAdapter {
  readonly #apiKey: string;
  readonly #serverURL: string;
  readonly #cache: Map<string, FunctionCallingTool[]> = new Map();
  readonly #core: GramAPICore;

  constructor(apiKey: string) {
    this.#apiKey = apiKey;
    this.#serverURL = getServerUrlByKey(apiKey);
    this.#core = new GramAPICore({
      serverURL: this.#serverURL,
    });
  }

  async #fetchInstance(
    project: string,
    toolset: string,
    environment?: string | undefined
  ): Promise<GetInstanceResult> {
    return unwrapAsync(
      instancesGetBySlug(
        this.#core,
        {
          option2: {
            apikeyHeaderGramKey: this.#apiKey,
            projectSlugHeaderGramProject: project,
          },
        },
        {
          toolsetSlug: toolset,
          environmentSlug: environment,
        }
      )
    );
  }

  async tools({
    project,
    toolset,
    environment,
  }: GramInstanceRequest): Promise<FunctionCallingTool[]> {
    const key = `${project}:${toolset}:${environment || ""}`;

    if (this.#cache.has(key)) {
      return this.#cache.get(key)!;
    }

    const client = this.#core;
    const apiKey = this.#apiKey;
    const instance = await this.#fetchInstance(project, toolset, environment);

    const tools: FunctionCallingTool[] = [];

    for (const toolData of instance.tools) {
      const schema = toolData.schema ? JSON.parse(toolData.schema) : {};
      const execute = async (input: string) => {
        const security: Record<string, string> = {
          "gram-key": apiKey,
          "gram-project": project,
        };
        const headers: Record<string, string> = { ...security };

        if (isBrowserLike) {
          headers[
            "user-agent"
          ] = `gram-ai/vercel typescript ${SDK_METADATA.sdkVersion}`;
        }

        const retryConfig = {
          strategy: "backoff",
          retryConnectionErrors: true,
        } as const;

        const url = new URL(
          `${this.#serverURL}/rpc/instances.invoke/tool`
        );
        url.searchParams.set("tool_id", toolData.id);
        if (environment) {
          url.searchParams.set("environment_slug", environment);
        }
        const request = new Request(url, {
          method: "POST",
          headers,
          body: input,
        });

        const result = await client._do(request, {
          context: {
            baseURL: this.#serverURL,
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

        const json = await response.json();
        return JSON.stringify(json);
      };

      const functionCallingTool = {
        name: toolData.name,
        description: toolData.description,
        parameters: schema,
        execute,
      };

      tools.push(functionCallingTool);
    }

    this.#cache.set(key, tools);

    return tools;
  }
}
