import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import { Stack } from "@speakeasy-api/moonshine";
import {
  FileCode,
  MessageSquare,
  Network,
  SquareFunction,
  Store,
} from "lucide-react";

type RoutesWithGoTo = ReturnType<typeof useRoutes>;

const ChoiceCard = ({
  onClick,
  icon: Icon,
  title,
  description,
}: {
  onClick: () => void;
  icon: React.ComponentType<{ className?: string; strokeWidth?: number }>;
  title: string;
  description: string;
}) => {
  return (
    <button
      onClick={onClick}
      className="bg-secondary hover:bg-accent group relative flex flex-col items-start rounded-lg p-5 text-left shadow-[inset_0px_1px_1px_0px_rgba(255,255,255,0.24),inset_0px_-1px_1px_0px_rgba(0,0,0,0.08)] transition-colors"
    >
      <Icon className="text-primary mb-2 h-6 w-6 shrink-0" strokeWidth={1.5} />
      <div className="flex flex-col gap-1">
        <Type className="text-heading-sm">{title}</Type>
        <Type small className="text-muted">
          {description}
        </Type>
      </div>
    </button>
  );
};

export const InitialChoiceStep = ({
  routes,
  isFunctionsEnabled,
}: {
  routes: RoutesWithGoTo;
  isFunctionsEnabled: boolean;
}): JSX.Element => {
  const telemetry = useTelemetry();

  const onChoiceSelected = (
    choice:
      | "connect_to_data"
      | "deploy_data_integrated_chat"
      | "connect_to_popular_mcps"
      | "connect_to_custom_remote_mcp",
  ) => {
    telemetry.capture("onboarding_choice_selected", { choice });
    if (choice === "deploy_data_integrated_chat") {
      telemetry.capture("elements_event", {
        action: "elements_onboarding_choice_selected",
      });
    }
  };

  return (
    <>
      <Stack gap={1}>
        <span className="text-heading-md">Get Started with the platform</span>
        <span className="text-body-sm">What would you like to do?</span>
      </Stack>
      <div className="grid grid-cols-1 gap-4">
        <ChoiceCard
          onClick={() => {
            onChoiceSelected("connect_to_popular_mcps");
            routes.catalog.goTo();
          }}
          icon={Store}
          title="Connect to Popular MCPs"
          description="Browse and connect to official and community-maintained MCP servers"
        />
        <ChoiceCard
          onClick={() => {
            onChoiceSelected("connect_to_custom_remote_mcp");
            routes.sources.addRemoteMcp.goTo();
          }}
          icon={Network}
          title="Custom Remote MCP"
          description="Connect to an existing MCP server by URL"
        />
        <ChoiceCard
          onClick={() => {
            onChoiceSelected("connect_to_data");
            routes.sources.addOpenAPI.goTo();
          }}
          icon={FileCode}
          title="Start from API"
          description="Generate tools from your OpenAPI specification"
        />
        {isFunctionsEnabled && (
          <ChoiceCard
            onClick={() => {
              onChoiceSelected("connect_to_data");
              routes.sources.addFunction.goTo();
            }}
            icon={SquareFunction}
            title="Start from Code"
            description="Deploy custom functions using the CLI"
          />
        )}
        <ChoiceCard
          onClick={() => {
            onChoiceSelected("deploy_data_integrated_chat");
            routes.elements.goTo();
          }}
          icon={MessageSquare}
          title="Deploy Chat Connected To Your Data"
          description="Build embeddable chat experiences powered by your data"
        />
      </div>
    </>
  );
};
