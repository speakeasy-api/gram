import { useRoutes } from "@/routes";
import { cn } from "@/lib/utils";
import { Toolset } from "@gram/client/models/components";
import { TooltipProvider, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip";
import { AlertTriangle, Check } from "lucide-react";
import { Tooltip } from "@/components/ui/tooltip";
import { useEnvironments } from "../environments/Environments";
import { Badge } from "@/components/ui/badge";

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
  
    if (!toolset) {
      return <Badge size={size} isLoading />;
    }
  
    const defaultEnvironment = environments.find(
      (env) => env.slug === toolset.defaultEnvironmentSlug
    );
  
    // We consider a toolset to need env vars if it has relevant environment variables and the default environment is set
    // The environment does not have any variables from the toolset's relevant environment variables set
    const needsEnvVars =
      defaultEnvironment &&
      toolset.relevantEnvironmentVariables &&
      toolset.relevantEnvironmentVariables.length > 0 &&
      !toolset.relevantEnvironmentVariables.some((varName) =>
        defaultEnvironment.entries.some(
          (entry) =>
            entry.name === varName &&
            entry.value !== "" &&
            entry.value !== "<EMPTY>"
        )
      );
  
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
  
    return (
      toolset.defaultEnvironmentSlug && (
        <routes.environments.environment.Link
          params={[toolset.defaultEnvironmentSlug]}
        >
          <Badge
            size={size}
            variant={variant}
            className={"flex items-center gap-1"}
          >
            {defaultEnvironment &&
              (needsEnvVars ? (
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div>
                        <AlertTriangle className={cn("w-4 h-4", colors.warn)} />
                      </div>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>
                        You have not set environment variables for this toolset.
                        Navigate to the environment and use fill for toolset.
                      </p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              ) : (
                <Check className={cn("w-4 h-4 stroke-3", colors.success)} />
              ))}
            Default Env
          </Badge>
        </routes.environments.environment.Link>
      )
    );
  };