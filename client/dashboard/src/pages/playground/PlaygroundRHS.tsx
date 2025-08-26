import { Message } from "@ai-sdk/react";
import { useListEnvironments } from "@gram/client/react-query";
import { EnvironmentDropdown } from "../environments/EnvironmentDropdown";
import { ChatConfig, ChatWindow } from "./ChatWindow";
import { PanelHeader } from "./Playground";

export function PlaygroundRHS({
  configRef,
  dynamicToolset,
  setSelectedEnvironment,
  initialPrompt,
}: {
  configRef: ChatConfig;
  dynamicToolset: boolean;
  setSelectedEnvironment: (environment: string) => void;
  initialPrompt?: string | null;
}) {
  const { data: environmentsData } = useListEnvironments();
  const selectedEnvironment = configRef.current.environmentSlug;

  const environments = environmentsData?.environments;
  const initialMessages: Message[] = [
    {
      id: "1",
      role: "system",
      content:
        "This chat has access to the selected toolset on the left! Use it to test out your toolset.",
    },
  ];

  return (
    <>
      <PanelHeader side="right">
        {environments && environments.length > 0 && (
          <EnvironmentDropdown
            label="Environment"
            tooltip="Set the active environment"
            selectedEnvironment={selectedEnvironment}
            setSelectedEnvironment={setSelectedEnvironment}
          />
        )}
      </PanelHeader>
      <div className="h-[calc(100%-61px)] pl-8 pr-4 pt-4">
        <ChatWindow
          configRef={configRef}
          dynamicToolset={dynamicToolset}
          initialMessages={initialMessages}
          initialPrompt={initialPrompt}
        />
      </div>
    </>
  );
}
