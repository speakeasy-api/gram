import type { Meta, StoryObj } from "@storybook/react-vite";
import { Editable } from "@/components/ui/editable";
import { Type } from "@/components/ui/type";

/**
 * `Editable` wraps arbitrary content and, on hover, blurs it and swaps in a
 * "pencil + Edit" (or "Can't edit") affordance. Hover the content in the
 * canvas below to see the effect — Storybook's static screenshot only shows
 * the resting state.
 */
const meta: Meta<typeof Editable> = {
  title: "UI/Editable",
  component: Editable,
  parameters: {
    layout: "padded",
  },
};

export default meta;

type Story = StoryObj<typeof Editable>;

export const Default: Story = {
  render: () => (
    <Editable onClick={() => alert("Edit clicked")}>
      <Type variant="subheading">Production API Server</Type>
    </Editable>
  ),
};

export const Disabled: Story = {
  render: () => (
    <Editable disabled onClick={() => alert("This should not fire")}>
      <Type variant="subheading">Locked Server Name</Type>
    </Editable>
  ),
};

export const RichContent: Story = {
  render: () => (
    <Editable onClick={() => alert("Edit description")} className="max-w-sm">
      <div className="rounded-md border p-4">
        <Type variant="subheading">Description</Type>
        <Type small muted className="mt-1">
          Handles inbound webhook events and routes them to the appropriate
          downstream tool.
        </Type>
      </div>
    </Editable>
  ),
};

export const List: Story = {
  render: () => (
    <div className="flex max-w-sm flex-col gap-2">
      {["Display Name", "Slug", "Description"].map((label) => (
        <Editable key={label} onClick={() => alert(`Edit ${label}`)}>
          <div className="flex items-center justify-between rounded-md border px-3 py-2">
            <Type small muted>
              {label}
            </Type>
            <Type small>Example value</Type>
          </div>
        </Editable>
      ))}
    </div>
  ),
};
