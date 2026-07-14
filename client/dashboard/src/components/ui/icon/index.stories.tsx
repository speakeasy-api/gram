import type { Meta, StoryObj } from "@storybook/react-vite";
import { Icon } from ".";
import { customIconNames, iconNames } from "./names";
import { sizes } from "@/components/ui/lib/types";

type Story = StoryObj<typeof Icon>;

const meta: Meta<typeof Icon> = {
  title: "UI/Icon",
  component: Icon,
  argTypes: {
    name: {
      control: "select",
      options: iconNames,
    },
    size: {
      control: "select",
      options: sizes,
    },
  },
};

export default meta;

export const Default: Story = {
  args: {
    name: "plus",
    size: "xl",
  },
};

export const Responsive: Story = {
  args: {
    name: "plus",
    size: {
      xs: "small",
      sm: "small",
      md: "medium",
      lg: "large",
      xl: "xl",
      "2xl": "2xl",
    },
  },
};

export const Animate: Story = {
  args: {
    name: "baby",
    size: "2xl",
    stroke: "blue",
    className: "animate-bounce",
  },
  decorators: [(Story) => <div className="p-10">{Story()}</div>],
};

export const WithCustomSize: Story = {
  args: {
    name: "chevron-right",
    size: "2xl",
  },
};

export const CustomIcons: Story = {
  argTypes: {
    name: {
      control: "select",
      options: customIconNames,
    },
  },
  args: {
    name: "npm",
    size: "large",
  },
};
