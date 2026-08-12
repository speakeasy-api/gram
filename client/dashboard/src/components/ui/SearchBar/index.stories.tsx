import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { SearchBar } from ".";

const meta: Meta<typeof SearchBar> = {
  title: "Design System/SearchBar",
  component: SearchBar,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof SearchBar>;

export const Default: Story = {
  render: function Render() {
    const [value, setValue] = useState("");

    return (
      <div className="w-96">
        <SearchBar
          value={value}
          onChange={setValue}
          placeholder="Search tools"
        />
      </div>
    );
  },
};
