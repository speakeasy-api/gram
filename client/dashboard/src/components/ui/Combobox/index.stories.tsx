import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { Combobox, type DropdownItem } from ".";

const meta: Meta<typeof Combobox> = {
  title: "Design System/Combobox",
  component: Combobox,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Combobox>;

const ENVIRONMENTS: DropdownItem[] = [
  { value: "prod", label: "Production" },
  { value: "staging", label: "Staging" },
  { value: "dev", label: "Development" },
];

export const Default: Story = {
  render: function Render() {
    const [selected, setSelected] = useState<DropdownItem | undefined>(
      ENVIRONMENTS[0],
    );

    return (
      <Combobox
        items={ENVIRONMENTS}
        selected={selected}
        onSelectionChange={setSelected}
        label="Environment"
      >
        {selected?.label ?? "Pick an environment"}
      </Combobox>
    );
  },
};
