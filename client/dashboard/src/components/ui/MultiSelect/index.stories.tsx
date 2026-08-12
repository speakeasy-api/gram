import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { MultiSelect } from ".";

const meta: Meta<typeof MultiSelect> = {
  title: "Design System/MultiSelect",
  component: MultiSelect,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof MultiSelect>;

const OPTIONS = [
  { value: "listPets", label: "listPets" },
  { value: "createPet", label: "createPet" },
  { value: "deletePet", label: "deletePet" },
];

export const Default: Story = {
  render: function Render() {
    const [value, setValue] = useState<string[]>(["listPets"]);

    return (
      <div className="w-96">
        <MultiSelect
          options={OPTIONS}
          value={value}
          onValueChange={setValue}
          placeholder="Select tools"
        />
      </div>
    );
  },
};
