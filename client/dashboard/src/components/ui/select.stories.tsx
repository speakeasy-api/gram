import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

const meta: Meta<typeof Select> = {
  title: "UI/Select",
  component: Select,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Select>;

export const Default: Story = {
  render: () => (
    <Select defaultValue="python">
      <SelectTrigger className="w-56">
        <SelectValue placeholder="Select a language" />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="python">Python</SelectItem>
        <SelectItem value="typescript">TypeScript</SelectItem>
        <SelectItem value="go">Go</SelectItem>
        <SelectItem value="java">Java</SelectItem>
      </SelectContent>
    </Select>
  ),
};

export const SmallSize: Story = {
  render: () => (
    <Select defaultValue="asc">
      <SelectTrigger size="sm" className="w-40">
        <SelectValue placeholder="Sort order" />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="asc">Ascending</SelectItem>
        <SelectItem value="desc">Descending</SelectItem>
      </SelectContent>
    </Select>
  ),
};

export const WithDescriptions: Story = {
  render: () => (
    <Select defaultValue="viewer">
      <SelectTrigger className="w-64">
        <SelectValue placeholder="Select a role" />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="admin" description="Full access to all resources">
          Admin
        </SelectItem>
        <SelectItem value="editor" description="Can create and edit resources">
          Editor
        </SelectItem>
        <SelectItem value="viewer" description="Read-only access">
          Viewer
        </SelectItem>
      </SelectContent>
    </Select>
  ),
};

export const DisabledSelect: Story = {
  render: () => (
    <Select defaultValue="python" disabled>
      <SelectTrigger className="w-56">
        <SelectValue placeholder="Select a language" />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="python">Python</SelectItem>
        <SelectItem value="typescript">TypeScript</SelectItem>
      </SelectContent>
    </Select>
  ),
};

export const DisabledItem: Story = {
  render: () => (
    <Select defaultValue="free">
      <SelectTrigger className="w-56">
        <SelectValue placeholder="Select a plan" />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="free">Free</SelectItem>
        <SelectItem value="pro">Pro</SelectItem>
        <SelectItem value="enterprise" disabled>
          Enterprise (contact sales)
        </SelectItem>
      </SelectContent>
    </Select>
  ),
};

function ControlledSelect() {
  const [value, setValue] = useState("staging");

  return (
    <div className="flex flex-col gap-2">
      <Select value={value} onValueChange={setValue}>
        <SelectTrigger className="w-56">
          <SelectValue placeholder="Select an environment" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="production">Production</SelectItem>
          <SelectItem value="staging">Staging</SelectItem>
          <SelectItem value="development">Development</SelectItem>
        </SelectContent>
      </Select>
      <p className="text-muted-foreground text-xs">Selected: {value}</p>
    </div>
  );
}

export const Controlled: Story = {
  render: () => <ControlledSelect />,
};
