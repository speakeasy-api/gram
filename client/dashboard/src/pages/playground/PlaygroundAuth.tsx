import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Type } from "@/components/ui/type";
import { Toolset } from "@/lib/toolTypes";
import { useRoutes } from "@/routes";
import { useMemo } from "react";

interface PlaygroundAuthProps {
  toolset: Toolset;
  environment?: {
    slug: string;
    entries?: Array<{ name: string; value: string }>;
  };
}

const SECRET_FIELD_INDICATORS = ["SECRET", "KEY", "TOKEN", "PASSWORD"] as const;
const PASSWORD_MASK = "••••••••";

export function getAuthStatus(
  toolset: Pick<
    Toolset,
    | "securityVariables"
    | "serverVariables"
    | "functionEnvironmentVariables"
    | "externalMcpHeaderDefinitions"
  >,
  environment?: { entries?: Array<{ name: string; value: string }> },
): { hasMissingAuth: boolean; missingCount: number } {
  // In playground, always filter out server_url variables since they can't be configured here
  const relevantEnvVars = [
    ...(toolset?.securityVariables?.flatMap((secVar) => secVar.envVariables) ??
      []),
    ...(toolset?.serverVariables?.flatMap((serverVar) =>
      serverVar.envVariables.filter(
        (v) => !v.toLowerCase().includes("server_url"),
      ),
    ) ?? []),
    ...(toolset?.functionEnvironmentVariables?.map((fnVar) => fnVar.name) ??
      []),
    ...(toolset?.externalMcpHeaderDefinitions?.map(
      (headerDef) => headerDef.name,
    ) ?? []),
  ];

  const missingCount = relevantEnvVars.filter((varName) => {
    const entry = environment?.entries?.find((e) => e.name === varName);
    return !entry?.value || entry.value.trim() === "";
  }).length;

  return {
    hasMissingAuth: missingCount > 0,
    missingCount,
  };
}

export function PlaygroundAuth({ toolset, environment }: PlaygroundAuthProps) {
  const routes = useRoutes();

  const relevantEnvVars = useMemo(() => {
    const securityVars =
      toolset?.securityVariables?.flatMap((secVar) => secVar.envVariables) ??
      [];
    // In playground, always filter out server_url variables since they can't be configured here
    const serverVars =
      toolset?.serverVariables?.flatMap((serverVar) =>
        serverVar.envVariables.filter(
          (v) => !v.toLowerCase().includes("server_url"),
        ),
      ) ?? [];
    const functionEnvVars =
      toolset?.functionEnvironmentVariables?.map((fnVar) => fnVar.name) ?? [];
    const externalMcpHeaderVars =
      toolset?.externalMcpHeaderDefinitions?.map(
        (headerDef) => headerDef.name,
      ) ?? [];

    return [
      ...securityVars,
      ...serverVars,
      ...functionEnvVars,
      ...externalMcpHeaderVars,
    ];
  }, [
    toolset?.securityVariables,
    toolset?.serverVariables,
    toolset.functionEnvironmentVariables,
    toolset.externalMcpHeaderDefinitions,
  ]);

  if (relevantEnvVars.length === 0) {
    return (
      <div className="text-center py-4">
        <Type variant="small" className="text-muted-foreground">
          No authentication required
        </Type>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {relevantEnvVars.map((varName) => {
        const entry =
          environment?.entries?.find((e) => e.name === varName) ?? null;
        const isSecret = SECRET_FIELD_INDICATORS.some((indicator) =>
          varName.toUpperCase().includes(indicator),
        );
        const hasExistingValue =
          entry?.value != null && entry.value.trim() !== "";
        const displayValue = hasExistingValue
          ? isSecret
            ? PASSWORD_MASK
            : entry.value
          : "";

        return (
          <div key={varName} className="space-y-1.5">
            <Label htmlFor={`auth-${varName}`} className="text-xs font-medium">
              {varName}
            </Label>
            <Input
              id={`auth-${varName}`}
              value={displayValue}
              placeholder={hasExistingValue ? "Configured" : "Not set"}
              type={isSecret ? "password" : "text"}
              className="font-mono text-xs h-7"
              readOnly
              disabled
            />
          </div>
        );
      })}
      <Type variant="small" className="text-muted-foreground pt-2">
        Configure auth in the{" "}
        <routes.toolsets.toolset.Link
          params={[toolset.slug]}
          hash="auth"
          className="underline hover:text-foreground"
        >
          toolset settings
        </routes.toolsets.toolset.Link>
      </Type>
    </div>
  );
}
