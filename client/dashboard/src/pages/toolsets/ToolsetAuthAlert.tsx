import { useMemo } from "react";
import { Alert } from "@speakeasy-api/moonshine";

import { Toolset } from "@gram/client/models/components";
import { useEnvironments } from "../environments/Environments";

function useRelevantEnvVars(toolset: Toolset) {
  return useMemo(() => {
    const requiresServerURL = toolset.httpTools?.some(
      (tool) => !tool.defaultServerUrl
    );

    const securityVars = toolset?.securityVariables?.flatMap(secVar => secVar.envVariables) ?? [];
    const serverVars = toolset?.serverVariables?.flatMap(serverVar => 
      serverVar.envVariables.filter(v => 
        !v.toLowerCase().includes("server_url") || requiresServerURL
      )
    ) ?? [];
    
    return [...securityVars, ...serverVars];
  }, [toolset]);
}

// Types
interface EnvironmentEntry {
  name: string;
  value?: string | null;
}

function hasRequiredVars(
  relevantEnvVars: string[],
  entries?: EnvironmentEntry[]
): boolean {
  if (relevantEnvVars.length === 0 || !entries) return true;

  return relevantEnvVars.every((varName) => {
    return entries.some((entry) => {
      const entryPrefix = entry.name.split("_")[0];
      const varPrefix = varName.split("_")[0];
      return (
        entryPrefix === varPrefix &&
        entry.value != null &&
        entry.value.trim() !== ""
      );
    });
  });
}

interface ToolsetAuthAlertProps {
  toolset: Toolset;
  environmentSlug?: string;
  onConfigureClick: () => void;
  context?: "playground" | "toolset";
}

export function ToolsetAuthAlert({
  toolset,
  environmentSlug,
  onConfigureClick,
  context = "toolset",
}: ToolsetAuthAlertProps) {
  return context === "playground" ? (
    <PlaygroundAuthAlert
      toolset={toolset}
      environmentSlug={environmentSlug}
      onConfigureClick={onConfigureClick}
    />
  ) : (
    <ToolsetPageAuthAlert
      toolset={toolset}
      onConfigureClick={onConfigureClick}
    />
  );
}

interface PlaygroundAuthAlertProps {
  toolset: Toolset;
  environmentSlug?: string;
  onConfigureClick: () => void;
}

function PlaygroundAuthAlert({
  toolset,
  environmentSlug,
  onConfigureClick,
}: PlaygroundAuthAlertProps) {
  const environments = useEnvironments();
  const relevantEnvVars = useRelevantEnvVars(toolset);

  const envSlug = environmentSlug ?? toolset.defaultEnvironmentSlug;
  const environment = environments.find((env) => env.slug === envSlug);

  const hasAllRequiredVars = useMemo(() => {
    return hasRequiredVars(relevantEnvVars, environment?.entries);
  }, [relevantEnvVars, environment?.entries]);

  if (hasAllRequiredVars) return null;

  return (
    <Alert variant="warning" dismissible={false} className="rounded-xs">
      <span className="text-sm">
        Authentication required to use this toolset
        {environmentSlug
          ? ` in the ${environment?.name ?? envSlug} environment`
          : ""}
        .{" "}
        <button
          type="button"
          onClick={onConfigureClick}
          className="text-link-warning underline"
          aria-label="Set up authentication for this toolset"
        >
          Set up now
        </button>
      </span>
    </Alert>
  );
}

interface ToolsetPageAuthAlertProps {
  toolset: Toolset;
  onConfigureClick: () => void;
}

function ToolsetPageAuthAlert({
  toolset,
  onConfigureClick,
}: ToolsetPageAuthAlertProps) {
  const environments = useEnvironments();
  const relevantEnvVars = useRelevantEnvVars(toolset);

  const hasAllRequiredVars = useMemo(() => {
    if (relevantEnvVars.length === 0) return true;

    return environments.some((env) =>
      hasRequiredVars(relevantEnvVars, env.entries)
    );
  }, [relevantEnvVars, environments]);

  if (hasAllRequiredVars) return null;

  return (
    <Alert variant="warning" dismissible={true} className="rounded-xs">
      <span className="text-sm">
        Set environment variables to test this toolset in MCP clients using your
        GRAM_KEY.{" "}
        <button
          type="button"
          onClick={onConfigureClick}
          className="text-link-warning underline"
          aria-label="Set up authentication for this toolset"
        >
          Set up now
        </button>
      </span>
    </Alert>
  );
}
