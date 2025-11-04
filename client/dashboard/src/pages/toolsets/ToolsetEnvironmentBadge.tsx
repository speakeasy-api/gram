import { InputDialog } from "@/components/input-dialog";
import { Badge } from "@/components/ui/badge";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { UrgentWarningIcon } from "@/components/ui/urgent-warning-icon";
import { useTelemetry } from "@/contexts/Telemetry";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { EnvironmentEntryInput } from "@gram/client/models/components";
import {
  invalidateAllListEnvironments,
  useUpdateEnvironmentMutation,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { Check } from "lucide-react";
import { useEnvironments } from "../environments/Environments";
import { useState } from "react";
import { isHttpTool, Toolset } from "@/lib/toolTypes";

export const ToolsetEnvironmentBadge = ({
  toolset,
  environmentSlug,
  size = "md",
  variant = "default",
}: {
  toolset: Toolset | undefined;
  environmentSlug?: string;
  size?: "sm" | "md";
  variant?: "outline" | "default";
}) => {
  const environments = useEnvironments();
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();

  const [envVarsDialogOpen, setEnvVarsDialogOpen] = useState(false);
  const [envVars, setEnvVars] = useState<Record<string, string>>({});

  const updateEnvironmentMutation = useUpdateEnvironmentMutation({
    onSuccess: () => {
      telemetry.capture("environment_event", { action: "environment_updated" });
      invalidateAllListEnvironments(queryClient);
    },
  });

  if (!toolset) {
    return <Badge size={size} isLoading />;
  }

  const envSlug = environmentSlug
    ? environmentSlug
    : toolset.defaultEnvironmentSlug;

  const environment = environments.find((env) => env.slug === envSlug);

  const requiresServerURL = toolset.tools?.some(
    (tool) => isHttpTool(tool) && !tool.defaultServerUrl,
  );

  const relevantEnvVars: string[] = [
    ...new Set([
      // Security variables (no filtering)
      ...(toolset.securityVariables?.flatMap((secVar) => secVar.envVariables) ??
        []),
      // Function environment variables
      ...(toolset.functionEnvironmentVariables?.map((fnVar) => fnVar.name) ??
        []),
      // Server variables (filter server_url unless required)
      ...(toolset.serverVariables?.flatMap((serverVar) =>
        serverVar.envVariables.filter(
          (v) => !v.toLowerCase().includes("server_url") || requiresServerURL,
        ),
      ) ?? []),
    ]),
  ];

  const missingEnvVars =
    relevantEnvVars?.filter(
      (varName) =>
        !environment?.entries?.find((entry) => {
          const entryPrefix = entry.name.split("_")[0];
          const varPrefix = varName.split("_")[0];
          return entryPrefix === varPrefix;
        }),
    ) || [];
  const needsEnvVars = missingEnvVars.length > 0;

  const colors = {
    default: {
      warn: "dark:text-orange-800 text-orange-300",
      success: "dark:text-green-800 text-green-300",
    },
    outline: {
      warn: "text-orange-500",
      success: "text-green-500",
    },
  }[variant];

  if (needsEnvVars) {
    const submitEnvVars = () => {
      if (!environment) {
        throw new Error("Environment not found");
      }

      const envVarsToUpdate = missingEnvVars
        ?.map((envVar) => ({
          name: envVar,
          value: envVars[envVar],
        }))
        .filter((envVar): envVar is EnvironmentEntryInput => !!envVar.value);

      if (envVarsToUpdate) {
        updateEnvironmentMutation.mutate(
          {
            request: {
              slug: environment.slug,
              updateEnvironmentRequestBody: {
                entriesToUpdate: envVarsToUpdate,
                entriesToRemove: [],
              },
            },
          },
          {
            onError: (error) => {
              console.error("Failed to update environment variables:", error);
            },
          },
        );
      }
    };

    const dialog = (
      <InputDialog
        open={envVarsDialogOpen}
        onOpenChange={setEnvVarsDialogOpen}
        title="Environment Variables"
        description="Enter values for the environment variables in order to use this toolset."
        onSubmit={submitEnvVars}
        inputs={missingEnvVars!.map((envVar) => ({
          label: envVar,
          name: envVar,
          placeholder: "<EMPTY>",
          value: envVars[envVar] || "",
          validate: (value) =>
            value.length > 0 && value !== "<EMPTY>" && !value.includes(" "),
          onChange: (value) => {
            setEnvVars({ ...envVars, [envVar]: value });
          },
          optional:
            (envVar.includes("SERVER_URL") && !requiresServerURL) ||
            envVar.includes("TOKEN_URL"), // Generally not required unless tools have no server url
        }))}
      />
    );

    return (
      <>
        <SimpleTooltip tooltip="Your environment is missing variables required by this toolset. Click here to set them.">
          <Badge
            size={size}
            variant={"urgent-warning"}
            onClick={() => setEnvVarsDialogOpen(true)}
            className="cursor-pointer"
          >
            <UrgentWarningIcon />
            Environment
          </Badge>
        </SimpleTooltip>
        {dialog}
      </>
    );
  }

  return (
    envSlug && (
      <routes.environments.environment.Link params={[envSlug]}>
        <SimpleTooltip tooltip="The environment for this toolset is fully configured.">
          <Badge size={size} variant={variant}>
            <Check className={cn("w-4 h-4 stroke-3", colors.success)} />
            Environment
          </Badge>
        </SimpleTooltip>
      </routes.environments.environment.Link>
    )
  );
};
