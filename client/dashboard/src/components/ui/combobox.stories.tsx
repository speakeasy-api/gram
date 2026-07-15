import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { Combobox, type DropdownItem } from "@/components/ui/combobox";

const meta: Meta<typeof Combobox> = {
  title: "UI/Combobox",
  component: Combobox,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Combobox>;

const languages: DropdownItem[] = [
  { value: "typescript", label: "TypeScript" },
  { value: "go", label: "Go" },
  { value: "python", label: "Python" },
  { value: "rust", label: "Rust" },
];

const longList: DropdownItem[] = [
  { value: "us-east-1", label: "US East (N. Virginia)" },
  { value: "us-west-2", label: "US West (Oregon)" },
  { value: "eu-west-1", label: "EU (Ireland)" },
  { value: "eu-central-1", label: "EU (Frankfurt)" },
  { value: "ap-southeast-1", label: "Asia Pacific (Singapore)" },
  { value: "ap-northeast-1", label: "Asia Pacific (Tokyo)" },
];

function ControlledCombobox({
  items,
  label,
  placeholder = "Select an option",
  disabledMessage,
}: {
  items: DropdownItem[];
  label?: string;
  placeholder?: string;
  disabledMessage?: string;
}) {
  const [selected, setSelected] = useState<DropdownItem | undefined>(undefined);

  return (
    <Combobox
      items={items}
      selected={selected}
      onSelectionChange={setSelected}
      label={label}
      disabledMessage={disabledMessage}
    >
      {selected?.label ?? placeholder}
    </Combobox>
  );
}

export const Default: Story = {
  render: () => <ControlledCombobox items={languages} />,
};

export const WithLabel: Story = {
  render: () => <ControlledCombobox items={languages} label="Language" />,
};

export const SearchableLongList: Story = {
  render: () => (
    <ControlledCombobox items={longList} placeholder="Select a region" />
  ),
};

export const Disabled: Story = {
  render: () => (
    <ControlledCombobox
      items={languages}
      disabledMessage="You don't have permission to change this"
    />
  ),
};

function PreselectedCombobox() {
  const [selected, setSelected] = useState<DropdownItem | undefined>(
    languages[1],
  );

  return (
    <Combobox
      items={languages}
      selected={selected}
      onSelectionChange={setSelected}
    >
      {selected?.label}
    </Combobox>
  );
}

export const Preselected: Story = {
  render: () => <PreselectedCombobox />,
};
