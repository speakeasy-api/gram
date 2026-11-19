import type { Meta, StoryObj } from "@storybook/react-vite";
import { Icon } from ".";
import dynamicIconImports from "lucide-react/dynamicIconImports";
import type { IconName } from "./names";
import { sizes } from "@/components/ui/lib/types";

type Story = StoryObj<typeof Icon>;

// Storybook only: the app itself must not import this map at runtime.
const iconNames = Object.keys(dynamicIconImports) as IconName[];

const meta: Meta<typeof Icon> = {
  title: "Design System/Icon",
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
    name: "loader-circle",
    size: "2xl",
    className: "animate-spin",
  },
  decorators: [
    (Story) => (
      <div className="p-10">
        <Story />
      </div>
    ),
  ],
};

export const WithCustomSize: Story = {
  args: {
    name: "chevron-right",
    size: "2xl",
  },
};
