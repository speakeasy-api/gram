import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from ".";

const meta: Meta<typeof Tabs> = {
  title: "Design System/Tabs",
  component: Tabs,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Tabs>;

export const Default: Story = {
  render: () => (
    <Tabs defaultValue="tools" className="w-[32rem]">
      <TabsList>
        <TabsTrigger value="tools">Tools</TabsTrigger>
        <TabsTrigger value="settings">Settings</TabsTrigger>
      </TabsList>
      <TabsContent value="tools" className="pt-4 text-sm">
        12 tools in this server.
      </TabsContent>
      <TabsContent value="settings" className="pt-4 text-sm">
        Auth, headers and timeouts.
      </TabsContent>
    </Tabs>
  ),
};

export const PageLevel: Story = {
  render: () => (
    <Tabs defaultValue="overview" className="w-[32rem]">
      <PageTabsList>
        <PageTabsTrigger value="overview">Overview</PageTabsTrigger>
        <PageTabsTrigger value="logs">Logs</PageTabsTrigger>
      </PageTabsList>
      <TabsContent value="overview" className="pt-4 text-sm">
        Traffic, errors and latency.
      </TabsContent>
      <TabsContent value="logs" className="pt-4 text-sm">
        Recent tool calls.
      </TabsContent>
    </Tabs>
  ),
};
