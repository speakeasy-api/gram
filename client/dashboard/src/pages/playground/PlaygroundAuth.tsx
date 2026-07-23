import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { PrivateInput } from "@/components/ui/private-input";
import { Type } from "@/components/ui/type";
import { useMissingRequiredEnvVars } from "@/hooks/useMissingEnvironmentVariables";
import { Toolset } from "@/lib/toolTypes";
import { useRoutes } from "@/routes";
import { useGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";
import { useListEnvironments } from "@gram/client/react-query/listEnvironments.js";
import { Badge, Stack } from "@speakeasy-api/moonshine";
import { CheckCircle, ExternalLink, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { usePlaygroundEnvironment } from "./usePlaygroundEnvironment";
import {
  environmentHasValue,
  getValueForEnvironment,
} from "../mcp/environmentVariableUtils";
import { useEnvironmentVariables } from "../mcp/useEnvironmentVariables";
import { usePlaygroundIssuerConnection } from "./usePlaygroundIssuerConnection";

interface PlaygroundAuthProps {
  toolset: Toolset;
  onPlaygroundEnvironmentSlug?: (slug: string | undefined) => void;
}

const PASSWORD_MASK = "••••••••";

/**
 * Connection status for an issuer-gated toolset. Mints a user-session JWT and
 * probes `/mcp/{slug}` to decide connected vs. not-connected (see
 * usePlaygroundIssuerConnection), then offers a first-party connect button that
 * opens the IDP login in a new tab. Returning focus re-probes so a freshly
 * linked upstream session flips the badge to Connected without a manual refresh.
 */
function IssuerLoginConnection({
  toolset,
  providerName,
}: {
  toolset: Toolset;
  providerName: string;
}) {
  const { connected, needsAuth, isLoading, refetch, connect, canConnect } =
    usePlaygroundIssuerConnection(toolset);

  // Re-probe when the user returns from the connect tab so a newly linked
  // session surfaces without a manual refresh.
  useEffect(() => {
    if (!needsAuth) return;
    const onFocus = () => refetch();
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [needsAuth, refetch]);

  return (
    <div className="bg-muted/30 rounded-md border p-3">
      <Stack gap={2}>
        <Stack
          direction="horizontal"
          align="center"
          className="justify-between"
        >
          <Type variant="small" className="font-medium">
            Login
          </Type>
          {isLoading ? (
            <Loader2 className="text-muted-foreground size-4 animate-spin" />
          ) : connected ? (
            <Badge variant="success">
              <CheckCircle className="mr-1 size-3" />
              Connected
            </Badge>
          ) : (
            <Badge variant="warning">Not Connected</Badge>
          )}
        </Stack>

        <Type variant="small" className="text-muted-foreground">
          {providerName}
        </Type>

        {!connected && !isLoading && (
          <Button
            size="sm"
            variant="default"
            className="w-full"
            onClick={connect}
            disabled={!canConnect}
          >
            <ExternalLink className="mr-2 size-3" />
            Connect
          </Button>
        )}
      </Stack>
    </div>
  );
}

export function PlaygroundAuth({
  toolset,
  onPlaygroundEnvironmentSlug,
}: PlaygroundAuthProps): JSX.Element {
  const routes = useRoutes();

  // Issuer-gated toolsets carry a user_session_issuer; interactive auth is the
  // first-party connect flow surfaced by IssuerLoginConnection below.
  const loginSecured = !!toolset.userSessionIssuerSlug;

  // Use the same environment data fetching as MCPAuthenticationTab
  const { data: environmentsData } = useListEnvironments();
  const environments = environmentsData?.environments ?? [];

  const { data: mcpMetadataData } = useGetMcpMetadata(
    { toolsetSlug: toolset.slug },
    undefined,
    {
      throwOnError: false,
      retry: false,
    },
  );
  const mcpMetadata = mcpMetadataData?.metadata;
  const defaultEnvironment = environments.find(
    (env) => env.id === mcpMetadata?.defaultEnvironmentId,
  );
  const defaultEnvironmentSlug = defaultEnvironment?.slug ?? "default";
  const defaultEnvironmentName = defaultEnvironment?.name;

  // Load environment variables using the same hook as MCPAuthenticationTab
  const envVars = useEnvironmentVariables(toolset, environments, mcpMetadata);

  // Track user-provided header values
  const [userProvidedValues, setUserProvidedValues] = useState<
    Record<string, string>
  >({});
  const [editedKeys, setEditedKeys] = useState<Set<string>>(new Set());
  const playgroundEnv = usePlaygroundEnvironment(toolset);

  // Calculate missing required variables using the same hook as MCPAuthenticationTab
  const missingRequiredCount = useMissingRequiredEnvVars(
    toolset,
    environments,
    defaultEnvironmentSlug,
    mcpMetadata,
  );

  // Notify parent component of the playground environment slug.
  // The cleanup clears the slug on unmount so the parent never holds
  // a stale value while remounting (e.g. on toolset switch via the
  // key={toolset.slug} prop).
  useEffect(() => {
    onPlaygroundEnvironmentSlug?.(
      playgroundEnv.exists ? playgroundEnv.slug : undefined,
    );
    return () => {
      onPlaygroundEnvironmentSlug?.(undefined);
    };
  }, [playgroundEnv.exists, playgroundEnv.slug, onPlaygroundEnvironmentSlug]);

  const handleSave = async () => {
    const entriesToUpdate = Object.entries(userProvidedValues)
      .filter(([key, value]) => value.trim() && editedKeys.has(key))
      .map(([name, value]) => ({ name, value }));
    // Only request removal of keys that actually have a stored value
    // on the server. This avoids sending phantom deletes when a user
    // typed then cleared a field that was never persisted.
    const storedKeys = new Set(
      playgroundEnv.storedEntries
        .filter((e) => e.hasStoredValue)
        .map((e) => e.name),
    );
    const entriesToRemove = Array.from(editedKeys).filter(
      (key) => !userProvidedValues[key]?.trim() && storedKeys.has(key),
    );
    if (entriesToUpdate.length === 0 && entriesToRemove.length === 0) {
      // Nothing meaningful to persist — just clear local edit state.
      setEditedKeys(new Set());
      setUserProvidedValues({});
      return;
    }
    try {
      const result = await playgroundEnv.save(entriesToUpdate, entriesToRemove);
      // On first-time create, propagate the slug to the parent
      // immediately so chat requests fired before the environments
      // list refetches still use the new playground environment.
      if (result.created) {
        onPlaygroundEnvironmentSlug?.(playgroundEnv.slug);
      }
      // Only clear edited state on success — on failure, keep the typed
      // values so the user can retry without retyping.
      setEditedKeys(new Set());
      setUserProvidedValues({});
    } catch {
      // Error toast is already shown by usePlaygroundEnvironment.
    }
  };

  // Show "no auth required" only if there are no env vars AND no interactive login
  if (envVars.length === 0 && !loginSecured) {
    return (
      <div className="py-4 text-center">
        <Type variant="small" className="text-muted-foreground">
          No authentication required
        </Type>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {/* Environment indicator */}
      {defaultEnvironmentName && (
        <div className="flex items-center gap-1.5">
          <Type variant="small" className="text-muted-foreground">
            Environment:
          </Type>
          <Badge variant="neutral">{defaultEnvironmentName}</Badge>
        </div>
      )}

      {/* Interactive (first-party) login for issuer-gated toolsets */}
      {loginSecured && (
        <IssuerLoginConnection toolset={toolset} providerName={toolset.name} />
      )}

      {/* Environment Variables */}
      {envVars.map((envVar) => {
        // Use the same utilities as MCPAuthenticationTab to get values
        const hasValue = environmentHasValue(envVar, defaultEnvironmentSlug);
        const value = getValueForEnvironment(envVar, defaultEnvironmentSlug);

        // Get header display name override if it exists
        const envConfig = mcpMetadata?.environmentConfigs?.find(
          (config) => config.variableName === envVar.key,
        );
        const displayName = envConfig?.headerDisplayName || envVar.key;

        // Determine display value and editability based on state
        let displayValue = "";
        let placeholder = "Not set";
        let isEditable = false;

        if (envVar.state === "user-provided") {
          const storedEntry = playgroundEnv.storedEntries.find(
            (e) => e.name === envVar.key,
          );
          const isEdited = editedKeys.has(envVar.key);
          if (isEdited) {
            displayValue = userProvidedValues[envVar.key] || "";
          } else if (storedEntry?.hasStoredValue) {
            displayValue = PASSWORD_MASK;
          } else {
            displayValue = "";
          }
          placeholder = "Enter value here";
          isEditable = true;
        } else if (envVar.state === "omitted") {
          displayValue = "";
          placeholder = "Omitted";
          isEditable = false;
        } else if (envVar.state === "system" && hasValue && value) {
          displayValue = PASSWORD_MASK;
          placeholder = "Configured";
          isEditable = false;
        }

        return (
          <div key={envVar.id} className="space-y-1.5">
            <Label
              htmlFor={`auth-${envVar.id}`}
              className="text-xs font-medium"
            >
              {displayName}
            </Label>
            <PrivateInput
              id={`auth-${envVar.id}`}
              value={displayValue}
              onChange={(newValue) => {
                if (!isEditable) return;
                // If this is the first edit of a previously-masked field,
                // strip the PASSWORD_MASK prefix so it doesn't contaminate
                // the credential the user is typing.
                const wasEditedBefore = editedKeys.has(envVar.key);
                let cleanValue = newValue;
                if (!wasEditedBefore && newValue.startsWith(PASSWORD_MASK)) {
                  cleanValue = newValue.slice(PASSWORD_MASK.length);
                }
                setEditedKeys((prev) => new Set(prev).add(envVar.key));
                setUserProvidedValues((prev) => ({
                  ...prev,
                  [envVar.key]: cleanValue,
                }));
              }}
              placeholder={placeholder}
              className="h-7 font-mono text-xs"
              readOnly={!isEditable}
              disabled={!isEditable}
            />
          </div>
        );
      })}
      {envVars.some((v) => v.state === "user-provided") && (
        <Button
          size="sm"
          variant="default"
          className="w-full"
          onClick={() => void handleSave()}
          disabled={editedKeys.size === 0 || playgroundEnv.isSaving}
        >
          {playgroundEnv.isSaving ? (
            <Loader2 className="mr-2 size-3 animate-spin" />
          ) : null}
          Save
        </Button>
      )}
      {missingRequiredCount > 0 && (
        <Type variant="small" className="text-warning pt-2">
          {missingRequiredCount} required variable
          {missingRequiredCount !== 1 ? "s" : ""} not configured
        </Type>
      )}
      <Type variant="small" className="text-muted-foreground pt-2">
        <routes.mcp.details.Link
          params={[toolset.slug]}
          hash="authentication"
          className="hover:text-foreground underline"
        >
          Configure auth
        </routes.mcp.details.Link>
      </Type>
    </div>
  );
}
