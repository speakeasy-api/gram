import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
  InputGroupTextarea,
} from ".";

const meta: Meta<typeof InputGroup> = {
  title: "Design System/InputGroup",
  component: InputGroup,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof InputGroup>;

export const WithAddon: Story = {
  render: () => (
    <InputGroup className="w-96">
      <InputGroupAddon>https://</InputGroupAddon>
      <InputGroupInput placeholder="api.example.com" />
    </InputGroup>
  ),
};

export const WithButton: Story = {
  render: () => (
    <InputGroup className="w-96">
      <InputGroupInput placeholder="Search tools" />
      <InputGroupAddon align="inline-end">
        <InputGroupButton>Go</InputGroupButton>
      </InputGroupAddon>
    </InputGroup>
  ),
};

export const Multiline: Story = {
  render: () => (
    <InputGroup className="w-96">
      <InputGroupTextarea placeholder="Describe this toolset…" />
    </InputGroup>
  ),
};
