import type { Meta, StoryObj } from "@storybook/react-vite";
import { Score } from ".";

const meta: Meta<typeof Score> = {
  title: "Moonshine/Score",
  component: Score,
  tags: ["autodocs"],
  argTypes: {
    score: {
      control: "range",
      min: 0,
      max: 100,
      step: 1,
    },
    size: {
      options: ["small", "medium", "large", "xl", "2xl"],
      control: "select",
      description: "The size of the score component",
      type: "string",
    },
    showLabel: {
      defaultValue: true,
      description: "Whether to show the label",
      type: "boolean",
      control: "boolean",
    },
    thresholds: {
      control: false,
    },
  },
};

export default meta;

type Story = StoryObj<typeof Score>;

export const Default: Story = {
  args: {
    score: 75,
  },
};

export const Low: Story = {
  args: {
    score: 25,
  },
};

export const Mid: Story = {
  args: {
    score: 50,
  },
};

export const High: Story = {
  args: {
    score: 75,
  },
};

export const Small: Story = {
  args: {
    score: 50,
    size: "small",
  },
};

export const Medium: Story = {
  args: {
    score: 60,
    size: "medium",
  },
};

export const Large: Story = {
  args: {
    score: 75,
    size: "large",
  },
};

export const XL: Story = {
  args: {
    score: 25,
    size: "xl",
  },
};

export const WithoutLabel: Story = {
  args: {
    score: 75,
    size: "small",
    showLabel: false,
  },
};

export const WithCustomTrackColour: Story = {
  args: {
    score: 75,
    size: "small",
    trackColor: "darkblue",
  },
};

export const Animate: Story = {
  args: {
    score: 75,
    animate: true,
    size: "2xl",
    animationDuration: 150,
  },
};
