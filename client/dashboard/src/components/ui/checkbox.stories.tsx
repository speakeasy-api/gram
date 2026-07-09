import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";

const meta: Meta<typeof Checkbox> = {
  title: "UI/Checkbox",
  component: Checkbox,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Checkbox>;

export const Default: Story = {
  render: () => <Checkbox />,
};

export const Checked: Story = {
  render: () => <Checkbox defaultChecked />,
};

export const Disabled: Story = {
  render: () => (
    <div className="flex items-center gap-4">
      <Checkbox disabled />
      <Checkbox disabled defaultChecked />
    </div>
  ),
};

export const WithLabel: Story = {
  render: () => (
    <div className="flex items-center gap-2">
      <Checkbox id="terms" />
      <Label htmlFor="terms">Accept terms and conditions</Label>
    </div>
  ),
};

function SelectAllGroup() {
  const [values, setValues] = useState({
    a: true,
    b: false,
    c: false,
  });

  const checkedCount = Object.values(values).filter(Boolean).length;
  const allChecked = checkedCount === Object.keys(values).length;
  const selectAllState: boolean | "indeterminate" = allChecked
    ? true
    : checkedCount === 0
      ? false
      : "indeterminate";

  const toggleAll = (checked: boolean) => {
    setValues({ a: checked, b: checked, c: checked });
  };

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2 border-b pb-2">
        <Checkbox
          checked={selectAllState}
          onCheckedChange={(checked) => toggleAll(checked === true)}
        />
        <Label>Select all</Label>
      </div>
      <div className="flex items-center gap-2">
        <Checkbox
          checked={values.a}
          onCheckedChange={(checked) =>
            setValues((prev) => ({ ...prev, a: checked === true }))
          }
        />
        <Label>Tool A</Label>
      </div>
      <div className="flex items-center gap-2">
        <Checkbox
          checked={values.b}
          onCheckedChange={(checked) =>
            setValues((prev) => ({ ...prev, b: checked === true }))
          }
        />
        <Label>Tool B</Label>
      </div>
      <div className="flex items-center gap-2">
        <Checkbox
          checked={values.c}
          onCheckedChange={(checked) =>
            setValues((prev) => ({ ...prev, c: checked === true }))
          }
        />
        <Label>Tool C</Label>
      </div>
    </div>
  );
}

export const IndeterminateGroup: Story = {
  render: () => <SelectAllGroup />,
};
