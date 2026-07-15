import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { TextArea } from "@/components/ui/textarea";

const meta: Meta<typeof TextArea> = {
  title: "UI/TextArea",
  component: TextArea,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof TextArea>;

function ControlledTextArea() {
  const [value, setValue] = useState("Describe the issue you're seeing...");

  return (
    <div className="flex w-96 flex-col gap-2">
      <TextArea value={value} onChange={setValue} />
      <p className="text-muted-foreground text-xs">{value.length} characters</p>
    </div>
  );
}

export const Default: Story = {
  render: () => <ControlledTextArea />,
};

export const WithPlaceholder: Story = {
  render: () => (
    <TextArea
      className="w-96"
      placeholder="Add a description for this toolset..."
    />
  ),
};

export const Disabled: Story = {
  render: () => (
    <TextArea
      className="w-96"
      value="This field cannot currently be edited."
      disabled
      onChange={() => {}}
    />
  ),
};

export const CustomRows: Story = {
  render: () => (
    <TextArea
      className="w-96"
      rows={8}
      placeholder="A larger text area for longer content..."
    />
  ),
};

export const Required: Story = {
  render: () => (
    <div className="flex w-96 flex-col gap-1.5">
      <label htmlFor="notes" className="text-sm font-medium">
        Notes
      </label>
      <TextArea id="notes" required placeholder="This field is required" />
    </div>
  ),
};

export const Uncontrolled: Story = {
  render: () => (
    <TextArea
      className="w-96"
      defaultValue="Uncontrolled value set once on mount."
    />
  ),
};
