import { Alert, AlertProps } from ".";
import type { Decorator, Meta, StoryObj } from "@storybook/react-vite";
import { variants } from "./types";
import { fn } from "storybook/test";

const defaultDecorators: Decorator[] = [
  (Story) => (
    <div className="m-auto flex h-full max-w-xl flex-col gap-2">{Story()}</div>
  ),
];
const meta: Meta<typeof Alert> = {
  title: "Moonshine/Alert",
  component: Alert,
  tags: ["autodocs"],
  argTypes: {
    variant: {
      control: "select",
      options: variants,
    },
  },
};

export default meta;

type Story = StoryObj<typeof Alert>;

export const Default: Story = {
  args: {
    variant: "default",
    children: "This is an alert",
  },
  decorators: defaultDecorators,
};

export const Success: Story = {
  args: {
    variant: "success",
    children: "This is an alert",
  },
  decorators: defaultDecorators,
};

export const Error: Story = {
  args: {
    variant: "error",
    children: "This is an alert",
  },
  decorators: defaultDecorators,
};

export const Warning: Story = {
  args: {
    variant: "warning",
    children: "This is an alert",
  },
  decorators: defaultDecorators,
};

export const Feature: Story = {
  args: {
    variant: "feature",
    children: "This is an alert",
  },
  decorators: defaultDecorators,
};

export const Info: Story = {
  args: {
    variant: "info",
    children: "This is an alert",
  },
  decorators: defaultDecorators,
};

export const Inline: Story = {
  args: {
    variant: "default",
    inline: true,
    children: "This is an alert",
  },
  decorators: defaultDecorators,
};

export const Dismissible: Story = {
  args: {
    variant: "default",
    dismissible: true,
    children: "This is an alert",
    onDismiss: fn<NonNullable<AlertProps["onDismiss"]>>().mockImplementation(
      () => {
        console.log("dismissed");
      },
    ),
  },
  decorators: defaultDecorators,
};

export const Multiline: Story = {
  args: {
    variant: "error",
    dismissible: true,
    children: (
      <div className="my-1 flex flex-col">
        <h2 className="text-lg font-semibold">Whoops!</h2>
        <p className="text-sm">
          We couldn't find the API you were looking for.
        </p>
      </div>
    ),
  },
  decorators: defaultDecorators,
};

export const WithContainer: Story = {
  args: {
    variant: "default",
    useContainer: true,
    children: "This is an alert",
  },
  parameters: {
    decorators: [],
  },
  decorators: [(Story) => <div className="w-screen">{Story()}</div>],
};
