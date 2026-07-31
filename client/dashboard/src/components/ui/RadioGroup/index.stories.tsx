import type { Meta, StoryObj } from "@storybook/react-vite";

import { RadioGroup, RadioGroupItem } from ".";
import { Label } from "@/components/ui/Label";

const meta: Meta<typeof RadioGroup> = {
  title: "Design System/RadioGroup",
  component: RadioGroup,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof RadioGroup>;

export const Default: Story = {
  render: () => (
    <RadioGroup defaultValue="public" className="flex flex-col gap-2">
      {["public", "private"].map((value) => (
        <div key={value} className="flex items-center gap-2">
          <RadioGroupItem value={value} id={value} />
          <Label htmlFor={value} className="capitalize">
            {value}
          </Label>
        </div>
      ))}
    </RadioGroup>
  ),
};
