import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";
import { TagInput } from ".";

const meta: Meta<typeof TagInput> = {
  title: "Design System/TagInput",
  component: TagInput,
  decorators: [
    (Story) => (
      <div className="m-auto mt-20 max-w-96">
        <Story />
      </div>
    ),
  ],
};

export default meta;

type Story = StoryObj<typeof TagInput>;

function Controlled(props: { initial: string[]; error?: boolean }) {
  const [value, setValue] = useState(props.initial);
  return (
    <TagInput
      value={value}
      onChange={setValue}
      placeholder="Type a value and press comma"
      error={props.error}
    />
  );
}

export const Empty: Story = {
  render: () => <Controlled initial={[]} />,
};

export const WithTags: Story = {
  render: () => <Controlled initial={["claude", "codex", "aider"]} />,
};

export const Error: Story = {
  render: () => <Controlled initial={["../etc/passwd"]} error />,
};
