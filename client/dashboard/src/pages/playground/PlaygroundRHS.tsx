import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { Message } from "@ai-sdk/react";
import { Stack } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { SdkContent } from "../sdk/SDK";
import { AgentifyButton } from "./Agentify";
import { ChatConfig, ChatWindow } from "./ChatWindow";
import { PanelHeader } from "./Playground";

export function PlaygroundRHS({
  configRef,
  dynamicToolset,
}: {
  configRef: ChatConfig;
  dynamicToolset: boolean;
}) {
  const [activeTab, setActiveTab] = useState<"chat" | "agents">("chat");

  const agentifyButton = (
    <AgentifyButton
      toolsetSlug={configRef.current.toolsetSlug ?? ""}
      environmentSlug={configRef.current.environmentSlug ?? ""}
      key="agentify-button"
      onAgentify={() => setActiveTab("agents")}
    />
  );

  const initialMessages: Message[] = [
    {
      id: "1",
      role: "system",
      content:
        "This chat has access to the selected toolset on the left! Use it to test out your toolset.",
    },
  ];

  return (
    <Tabs
      value={activeTab}
      onValueChange={(value) => setActiveTab(value as "chat" | "agents")}
      className="h-full relative"
    >
      <PanelHeader side="right">
        <Stack direction="horizontal" gap={2} align="center">
          <Type className="font-medium">Use with: </Type>
          <TabsList>
            <TabsTrigger value="chat">Chat</TabsTrigger>
            <TabsTrigger value="agents">SDKs</TabsTrigger>
          </TabsList>
        </Stack>
      </PanelHeader>
      <div className="h-[calc(100%-61px)] pl-8 pr-4 pt-4">
        <TabsContent value="chat" className="h-full">
          <ChatWindow
            configRef={configRef}
            dynamicToolset={dynamicToolset}
            additionalActions={agentifyButton}
            initialMessages={initialMessages}
          />
        </TabsContent>
        <TabsContent
          value="agents"
          className="h-full overflow-scroll pb-4 pr-4"
        >
          <SdkContent
            toolset={configRef.current.toolsetSlug ?? undefined}
            environment={configRef.current.environmentSlug ?? undefined}
          />
        </TabsContent>
      </div>
    </Tabs>
  );
}
