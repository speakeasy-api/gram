import { Heading } from "@/components/ui/heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useToolsetSuspense } from "@gram/client/react-query";
import { Stack } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { McpToolsetCard } from "../mcp/MCP";
import { SdkContent } from "../sdk/SDK";
import { AgentifyButton } from "./Agentify";
import { ChatConfig, ChatWindow } from "./ChatWindow";
import { PanelHeader } from "./Playground";
import { Message } from "@ai-sdk/react";

export function PlaygroundRHS({
  configRef,
  dynamicToolset,
}: {
  configRef: ChatConfig;
  dynamicToolset: boolean;
}) {
  const [activeTab, setActiveTab] = useState<"chat" | "agents" | "mcp">("chat");

  const agentifyButton = (
    <AgentifyButton
      toolsetSlug={configRef.current.toolsetSlug ?? ""}
      environmentSlug={configRef.current.environmentSlug ?? ""}
      key="agentify-button"
      onAgentify={() => setActiveTab("agents")}
    />
  );

  const initialMessages: Message[] | undefined = configRef.current.isOnboarding
    ? [
        {
          id: "1",
          role: "assistant",
          content:
            "Welcome to Gram! Upload an OpenAPI document to get started.",
        },
      ]
    : undefined;

  return (
    <Tabs
      value={activeTab}
      onValueChange={(value) =>
        setActiveTab(value as "chat" | "agents" | "mcp")
      }
      className="h-full relative"
    >
      <PanelHeader side="right">
        <Stack direction="horizontal" gap={2} align="center">
          <Type className="font-medium">Use with: </Type>
          <TabsList className="bg-stone-200 dark:bg-stone-800">
            <TabsTrigger value="chat">Chat</TabsTrigger>
            <TabsTrigger value="mcp">MCP</TabsTrigger>
            <TabsTrigger value="agents">Agents</TabsTrigger>
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
        <TabsContent value="mcp">
          {configRef.current.toolsetSlug ? (
            <McpTab toolsetSlug={configRef.current.toolsetSlug} />
          ) : (
            <div className="text-muted-foreground">No toolset selected</div>
          )}
        </TabsContent>
      </div>
    </Tabs>
  );
}

const McpTab = ({ toolsetSlug }: { toolsetSlug: string }) => {
  const { data: toolset, refetch } = useToolsetSuspense({ slug: toolsetSlug });

  return (
    <Stack gap={8}>
      <div>
        <Heading variant="h2">Hosted MCP Servers</Heading>
        <Type className="text-muted-foreground mt-2">
          Expose this toolset as a hosted MCP server
        </Type>
      </div>
      <McpToolsetCard toolset={toolset} onUpdate={refetch} />
    </Stack>
  );
};
