import { Skeleton } from ".";
import type { Meta, StoryObj } from "@storybook/react-vite";

const meta: Meta<typeof Skeleton> = {
  title: "Moonshine/Skeleton",
  component: Skeleton,
};

export default meta;

type Story = StoryObj<typeof Skeleton>;

export const Default: Story = {
  args: {
    children: [
      <p key="paragraph-1">
        Lorem ipsum dolor sit amet consectetur. Lorem ipsum dolor sit amet
        consectetur.
      </p>,
      <p key="paragraph-2">
        Lorem ipsum dolor sit amet consectetur adipisicing elit. Lorem ipsum
        dolor sit amet consectetur adipisicing elit.
      </p>,
      <p key="paragraph-3">
        Lorem ipsum dolor sit amet consectetur adipisicing elit. Quisquam, quos.
      </p>,
    ],
  },
};

export const WithComplexChildren: Story = {
  render: (args) => (
    <div className="flex flex-col gap-2">
      <Skeleton {...args}>
        <p>
          Lorem ipsum dolor sit amet consectetur. Lorem ipsum dolor sit amet
          consectetur.
        </p>

        <div className="flex flex-row gap-2">
          {Array.from({ length: 20 }).map((_, i) => (
            <span key={i}>{i}</span>
          ))}
        </div>
      </Skeleton>
    </div>
  ),
};

export const WithEmptyChildren: Story = {
  args: {
    children: [
      <div key="block-1" className="h-5 w-60 rounded-lg bg-muted" />,
      <div key="block-2" className="h-5 w-48 rounded-lg bg-muted" />,
      <div key="block-3" className="h-5 w-40 rounded-lg bg-muted" />,
    ],
  },
};

export const WithClassName: Story = {
  args: {
    className: "bg-muted/50",
    children: [
      <p key="paragraph-1">
        Lorem ipsum dolor sit amet consectetur. Lorem ipsum dolor sit amet
        consectetur.
      </p>,
      <p key="paragraph-2">
        Lorem ipsum dolor sit amet consectetur adipisicing elit. Lorem ipsum
        dolor sit amet consectetur adipisicing elit.
      </p>,
      <p key="paragraph-3">
        Lorem ipsum dolor sit amet consectetur adipisicing elit. Quisquam, quos.
      </p>,
    ],
  },
};
