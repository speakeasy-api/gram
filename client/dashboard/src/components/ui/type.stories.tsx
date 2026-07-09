import type { Meta, StoryObj } from "@storybook/react-vite";

import { Type } from "@/components/ui/type";

const meta: Meta<typeof Type> = {
  title: "UI/Type",
  component: Type,
  tags: ["autodocs"],
  args: {
    children: "The quick brown fox jumps over the lazy dog",
  },
};

export default meta;

type Story = StoryObj<typeof Type>;

export const Body: Story = { args: { variant: "body" } };
export const Subheading: Story = { args: { variant: "subheading" } };
export const Small: Story = { args: { variant: "small" } };

export const AllVariants: Story = {
  render: () => (
    <div className="flex flex-col gap-2">
      <Type variant="subheading">Subheading text</Type>
      <Type variant="body">Body text</Type>
      <Type variant="small">Small text</Type>
    </div>
  ),
};

export const Modifiers: Story = {
  render: () => (
    <div className="flex flex-col gap-2">
      <Type muted>Muted text</Type>
      <Type italic>Italic text</Type>
      <Type mono>Monospace text</Type>
      <Type small>Small (shorthand) text</Type>
      <Type destructive>Destructive text</Type>
      <Type as="span">Rendered as a span</Type>
    </div>
  ),
};

export const LoadingSkeleton: Story = {
  render: () => (
    <div className="flex flex-col gap-2">
      <Type variant="body">{undefined}</Type>
      <Type variant="body" skeleton="phrase">
        {undefined}
      </Type>
      <Type variant="body" skeleton="line">
        {undefined}
      </Type>
      <Type variant="body" skeleton="paragraph">
        {undefined}
      </Type>
    </div>
  ),
};
