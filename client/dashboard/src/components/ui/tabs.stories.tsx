import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";

const meta: Meta<typeof Tabs> = {
  title: "UI/Tabs",
  component: Tabs,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Tabs>;

export const Default: Story = {
  render: () => (
    <Tabs defaultValue="overview" className="w-96">
      <TabsList>
        <TabsTrigger value="overview">Overview</TabsTrigger>
        <TabsTrigger value="activity">Activity</TabsTrigger>
        <TabsTrigger value="settings">Settings</TabsTrigger>
      </TabsList>
      <TabsContent value="overview">
        <p className="text-sm">High level summary of this resource.</p>
      </TabsContent>
      <TabsContent value="activity">
        <p className="text-sm">Recent activity and audit events.</p>
      </TabsContent>
      <TabsContent value="settings">
        <p className="text-sm">Configuration options for this resource.</p>
      </TabsContent>
    </Tabs>
  ),
};

export const WithDisabledTab: Story = {
  render: () => (
    <Tabs defaultValue="overview" className="w-96">
      <TabsList>
        <TabsTrigger value="overview">Overview</TabsTrigger>
        <TabsTrigger value="billing" disabled>
          Billing
        </TabsTrigger>
        <TabsTrigger value="settings">Settings</TabsTrigger>
      </TabsList>
      <TabsContent value="overview">
        <p className="text-sm">High level summary of this resource.</p>
      </TabsContent>
      <TabsContent value="billing">
        <p className="text-sm">Billing is unavailable on this plan.</p>
      </TabsContent>
      <TabsContent value="settings">
        <p className="text-sm">Configuration options for this resource.</p>
      </TabsContent>
    </Tabs>
  ),
};

export const PageStyleTabs: Story = {
  name: "Page-level tabs (underline style)",
  render: () => (
    <Tabs defaultValue="roles" className="w-[28rem]">
      <div className="border-border -mx-2 border-b px-2">
        <TabsList className="h-auto justify-start gap-4 rounded-none bg-transparent p-0 text-sm">
          <PageTabsTrigger value="roles">Roles (4)</PageTabsTrigger>
          <PageTabsTrigger value="members">Members (12)</PageTabsTrigger>
          <PageTabsTrigger value="challenges">
            Authorization Challenges
          </PageTabsTrigger>
        </TabsList>
      </div>
      <TabsContent value="roles" className="mt-4">
        <p className="text-sm">Roles defined for this organization.</p>
      </TabsContent>
      <TabsContent value="members" className="mt-4">
        <p className="text-sm">Members belonging to this organization.</p>
      </TabsContent>
      <TabsContent value="challenges" className="mt-4">
        <p className="text-sm">Pending authorization challenges.</p>
      </TabsContent>
    </Tabs>
  ),
};

function ControlledTabs() {
  const [tab, setTab] = useState("json");

  return (
    <div className="flex flex-col gap-2">
      <Tabs value={tab} onValueChange={setTab} className="w-96">
        <TabsList>
          <TabsTrigger value="json">JSON</TabsTrigger>
          <TabsTrigger value="yaml">YAML</TabsTrigger>
          <TabsTrigger value="raw">Raw</TabsTrigger>
        </TabsList>
        <TabsContent value="json">
          <pre className="bg-muted rounded-md p-2 text-xs">{"{ }"}</pre>
        </TabsContent>
        <TabsContent value="yaml">
          <pre className="bg-muted rounded-md p-2 text-xs">{"{}"}</pre>
        </TabsContent>
        <TabsContent value="raw">
          <pre className="bg-muted rounded-md p-2 text-xs">(empty)</pre>
        </TabsContent>
      </Tabs>
      <p className="text-muted-foreground text-xs">Active tab: {tab}</p>
    </div>
  );
}

export const Controlled: Story = {
  render: () => <ControlledTabs />,
};
