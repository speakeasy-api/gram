import { useCallback, useState } from "react";
import { toast } from "sonner";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import { useMarketplaceSettings } from "@gram/client/react-query/marketplaceSettings";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import { useProjectSlugForRequests, useSlugs } from "@/contexts/Sdk";
import { useOrgRoutes } from "@/routes";
import type { AgentPlatform, PlatformSetupStep } from "../types";

const API_KEY_PLACEHOLDER = "{{GRAM_API_KEY}}";
const PROJECT_SLUG_PLACEHOLDER = "{{GRAM_PROJECT_SLUG}}";
const MARKETPLACE_URL_PLACEHOLDER = "{{GRAM_MARKETPLACE_URL}}";
const REPO_URL_PLACEHOLDER = "{{GRAM_REPO_URL}}";
const REPO_NAME_PLACEHOLDER = "{{GRAM_REPO_NAME}}";
const REPO_OWNER_PLACEHOLDER = "{{GRAM_REPO_OWNER}}";
const CODEX_PLUGIN_NAME_PLACEHOLDER = "{{GRAM_CODEX_PLUGIN_NAME}}";
const CLAUDE_PLUGIN_NAME_PLACEHOLDER = "{{GRAM_CLAUDE_PLUGIN_NAME}}";
const CURSOR_PLUGIN_NAME_PLACEHOLDER = "{{GRAM_CURSOR_PLUGIN_NAME}}";
const MARKETPLACE_NAME_PLACEHOLDER = "{{GRAM_MARKETPLACE_NAME}}";
const DEVICE_AGENT_URL_PLACEHOLDER = "{{GRAM_DEVICE_AGENT_URL}}";

function applySubstitutions(
  input: string,
  substitutions: Array<[string, string]>,
): string {
  let out = input;
  for (const [marker, value] of substitutions) {
    out = out.split(marker).join(value);
  }
  return out;
}

export interface PlatformPlaceholders {
  /**
   * The step's snippet with every placeholder filled, or undefined while a
   * value it needs is still missing.
   */
  snippetFor: (step: PlatformSetupStep, apiKey?: string) => string | undefined;
  /** Fills a help link's URL. Never carries the API key — see below. */
  linkFor: (url: string) => string;
}

/** The org- and project-scoped values a platform's instructions interpolate. */
export function usePlatformPlaceholders(): PlatformPlaceholders {
  const { data: publishStatus } = usePublishStatus(undefined, undefined, {
    throwOnError: false,
  });
  const { data: marketplaceSettings } = useMarketplaceSettings(
    undefined,
    undefined,
    { throwOnError: false },
  );
  const { orgSlug = "" } = useSlugs();
  const projectSlug = useProjectSlugForRequests();
  const deviceAgentUrl = useOrgRoutes().deviceAgent.href();
  // The marketplace.json "name" field — what `enabledPlugins`/`plugins.required`
  // reference as the `<plugin>@<marketplace>` suffix. Sourced from the server
  // (naming.MarketplaceName is org-slug-normalized AND project-scoped) rather
  // than re-derived here, which previously hardcoded the wrong "-gram" suffix
  // instead of "-speakeasy" and ignored non-default-project scoping entirely.
  const marketplaceName = marketplaceSettings?.effectiveName ?? "";
  const marketplaceUrl = publishStatus?.marketplaceUrl ?? "";

  // The API key is a live secret and must never be interpolated into a URL —
  // an href leaks it via the address bar, Referer header, browser history, and
  // server logs. So it is absent here; only snippetFor adds it.
  const substitutions: Array<[string, string]> = [
    [PROJECT_SLUG_PLACEHOLDER, projectSlug],
    [MARKETPLACE_URL_PLACEHOLDER, marketplaceUrl],
    [REPO_URL_PLACEHOLDER, publishStatus?.repoUrl ?? ""],
    [REPO_NAME_PLACEHOLDER, publishStatus?.repoName ?? ""],
    [REPO_OWNER_PLACEHOLDER, publishStatus?.repoOwner ?? ""],
    [
      CODEX_PLUGIN_NAME_PLACEHOLDER,
      orgSlug ? `${orgSlug}-observability-codex` : "",
    ],
    [CLAUDE_PLUGIN_NAME_PLACEHOLDER, orgSlug ? `${orgSlug}-observability` : ""],
    [
      CURSOR_PLUGIN_NAME_PLACEHOLDER,
      orgSlug ? `${orgSlug}-observability-cursor` : "",
    ],
    [MARKETPLACE_NAME_PLACEHOLDER, marketplaceName],
    [DEVICE_AGENT_URL_PLACEHOLDER, deviceAgentUrl],
  ];

  return {
    snippetFor: (step, apiKey) => {
      if (!step.code) return undefined;
      // Withhold the snippet while a value it interpolates is still missing —
      // an unminted API key, or a marketplace name or marketplace URL that has
      // not resolved (both fall back to "") — otherwise users copy a snippet
      // carrying an empty Gram-Key, a malformed "<plugin>@" suffix, or an empty
      // marketplace URL. The repo owner, name and URL need no guard of their
      // own: getPublishStatus fills all three whenever it reports connected,
      // which is now a precondition of the marketplace counting as published.
      if (step.requiresApiKey && !apiKey) return undefined;
      if (
        step.code.includes(MARKETPLACE_NAME_PLACEHOLDER) &&
        !marketplaceName
      ) {
        return undefined;
      }
      if (step.code.includes(MARKETPLACE_URL_PLACEHOLDER) && !marketplaceUrl) {
        return undefined;
      }
      return applySubstitutions(step.code, [
        ...substitutions,
        [API_KEY_PLACEHOLDER, apiKey ?? ""],
      ]);
    },
    linkFor: (url) => applySubstitutions(url, substitutions),
  };
}

export interface PlatformApiKeys {
  keys: Record<string, string>;
  pending: Record<string, boolean>;
  errors: Record<string, string>;
  ensure: (platform: AgentPlatform) => void;
}

/** Mints the hooks-scoped API key a platform's snippets need, once each. */
export function usePlatformApiKeys(): PlatformApiKeys {
  const [keys, setKeys] = useState<Record<string, string>>({});
  const [pending, setPending] = useState<Record<string, boolean>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const createKeyMutation = useCreateAPIKeyMutation();

  const ensure = useCallback(
    (platform: AgentPlatform) => {
      const needsKey = platform.setupSteps.some((s) => s.requiresApiKey);
      if (!needsKey) return;
      if (keys[platform.id] || pending[platform.id]) return;

      setPending((prev) => ({ ...prev, [platform.id]: true }));
      setErrors((prev) => {
        const next = { ...prev };
        delete next[platform.id];
        return next;
      });

      // The api_keys table enforces UNIQUE (organization_id, name) on
      // non-deleted rows, and key tokens are only returned on creation (never
      // re-readable via listKeys). Both constraints mean we can't "fetch the
      // existing key" — we have to mint a fresh one with a unique name on each
      // run. The timestamp suffix tells admins which run produced each entry
      // in the API Keys list; a random suffix guarantees uniqueness even when
      // two tabs/remounts mint a key in the same second.
      const timestamp = new Date().toISOString().slice(0, 19).replace("T", " ");
      const uniqueSuffix = Math.random().toString(36).slice(2, 6);
      createKeyMutation.mutate(
        {
          security: { sessionHeaderGramSession: "" },
          request: {
            createKeyForm: {
              name: `${platform.name} hooks (setup ${timestamp} ${uniqueSuffix})`,
              scopes: ["hooks"],
            },
          },
        },
        {
          onSuccess: (data) => {
            setPending((prev) => ({ ...prev, [platform.id]: false }));
            if (data.key) {
              setKeys((prev) => ({ ...prev, [platform.id]: data.key! }));
            } else {
              setErrors((prev) => ({
                ...prev,
                [platform.id]: "API key token missing from response.",
              }));
            }
          },
          onError: (err) => {
            setPending((prev) => ({ ...prev, [platform.id]: false }));
            const msg =
              err instanceof Error
                ? err.message
                : "Failed to generate API key.";
            setErrors((prev) => ({ ...prev, [platform.id]: msg }));
            toast.error(`Failed to generate API key: ${msg}`);
          },
        },
      );
    },
    [keys, pending, createKeyMutation],
  );

  return { keys, pending, errors, ensure };
}
