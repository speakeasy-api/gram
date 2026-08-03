import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { PrivateInput } from ".";

const meta: Meta<typeof PrivateInput> = {
  title: "Design System/PrivateInput",
  component: PrivateInput,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof PrivateInput>;

export const Default: Story = {
  render: function Render() {
    const [value, setValue] = useState("gram_sk_live_9f2c");

    return (
      <div className="w-96">
        <PrivateInput value={value} onChange={setValue} />
      </div>
    );
  },
};
