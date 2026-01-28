import { useMemo } from "react";

import { Toolset } from "@/lib/toolTypes";
import { ToolsetEntry } from "@gram/client/models/components";

type ToolsetLike = (Toolset | ToolsetEntry) | undefined;

export function useToolsetEnvVars(
  toolset: ToolsetLike,
  requiresServerURL: boolean,
): string[] {
  return useMemo(() => {
    if (!toolset) return [];

    const securityVars =
      toolset.securityVariables?.flatMap((secVar) => secVar.envVariables) ?? [];

    const functionEnvVars =
      toolset.functionEnvironmentVariables?.map((fnVar) => fnVar.name) ?? [];

    const serverVars =
      toolset.serverVariables?.flatMap((serverVar) =>
        serverVar.envVariables.filter((envVar) => {
          if (requiresServerURL) return true;
          return !envVar.toLowerCase().includes("server_url");
        }),
      ) ?? [];

    const externalMcpHeaderVars =
      toolset.externalMcpHeaderDefinitions?.map(
        (headerDef) => headerDef.name,
      ) ?? [];

    return [
      ...new Set([
        ...securityVars,
        ...serverVars,
        ...functionEnvVars,
        ...externalMcpHeaderVars,
      ]),
    ];
  }, [toolset, requiresServerURL]);
}
