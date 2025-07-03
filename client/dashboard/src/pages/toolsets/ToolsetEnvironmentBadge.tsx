import { InputDialog } from "@/components/input-dialog";
import { Badge } from "@/components/ui/badge";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { useTelemetry } from "@/contexts/Telemetry";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { EnvironmentEntryInput, Toolset } from "@gram/client/models/components";
import { invalidateAllListEnvironments, useUpdateEnvironmentMutation } from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Check } from "lucide-react";
import { useEnvironments } from "../environments/Environments";
import { useState } from "react";

export const ToolsetEnvironmentBadge = ({
  toolset,
  size = "md",
  variant = "default",
}: {
  toolset: Toolset | undefined;
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

  const environment = environments.find(
    (env) => env.slug === toolset.defaultEnvironmentSlug
  );

  const requiresServerURL = toolset.httpTools?.some(
    (tool) => !tool.defaultServerUrl
  );

  // For now to reduce user confusion we omit server url env variables
  // If a spec already has a security env variable set we will not surface variables as missing for that spec
  const relevantEnvVars = toolset?.relevantEnvironmentVariables?.filter(
    (varName) => !varName.includes("SERVER_URL") || requiresServerURL
  );

  const missingEnvVars =
    relevantEnvVars?.filter(
      (varName) =>
        !environment?.entries?.find((entry) => {
          const entryPrefix = entry.name.split("_")[0];
          const varPrefix = varName.split("_")[0];
          return entryPrefix === varPrefix;
        })
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
              console.log("error", error);
            },
          }
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
          optional: (envVar.includes("SERVER_URL") && !requiresServerURL) || envVar.includes("TOKEN_URL"), // Generally not required unless tools have no server url
        }))}
      />
    );

    return (
      <>
        <SimpleTooltip tooltip="Your environment is missing variables required by this toolset. Click here to set them.">
          <Badge
            size={size}
            variant={"warning"}
            onClick={() => setEnvVarsDialogOpen(true)}
            className={
              "flex items-center cursor-pointer gap-1 ring-2 ring-orange-500 dark:ring-orange-700"
            }
          >
            <AlertTriangle
              className={cn("w-4 h-4 text-orange-700 dark:text-orange-300")}
            />
            Environment
          </Badge>
        </SimpleTooltip>
        {dialog}
      </>
    );
  }

  return (
    toolset.defaultEnvironmentSlug && (
      <routes.environments.environment.Link
        params={[toolset.defaultEnvironmentSlug]}
      >
        <SimpleTooltip tooltip="The default environment for this toolset is fully configured.">
          <Badge size={size} variant={variant}>
            <Check className={cn("w-4 h-4 stroke-3", colors.success)} />
            Environment
          </Badge>
        </SimpleTooltip>
      </routes.environments.environment.Link>
    )
  );
};
