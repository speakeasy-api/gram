import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { SearchBar } from "@/components/ui/search-bar";

const meta: Meta<typeof SearchBar> = {
  title: "UI/SearchBar",
  component: SearchBar,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof SearchBar>;

function ControlledSearchBar({
  placeholder,
  disabled,
}: {
  placeholder?: string;
  disabled?: boolean;
}) {
  const [value, setValue] = useState("");

  return (
    <SearchBar
      value={value}
      onChange={setValue}
      placeholder={placeholder}
      disabled={disabled}
      className="w-64"
    />
  );
}

export const Default: Story = {
  render: () => <ControlledSearchBar />,
};

export const CustomPlaceholder: Story = {
  render: () => <ControlledSearchBar placeholder="Search tools" />,
};

function PrefilledSearchBar() {
  const [value, setValue] = useState("audit log");

  return <SearchBar value={value} onChange={setValue} className="w-64" />;
}

export const WithValue: Story = {
  render: () => <PrefilledSearchBar />,
};

export const Disabled: Story = {
  render: () => <ControlledSearchBar disabled placeholder="Search disabled" />,
};
