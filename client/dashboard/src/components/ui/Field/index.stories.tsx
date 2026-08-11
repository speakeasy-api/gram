import type { Meta, StoryObj } from "@storybook/react-vite";

import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from ".";
import { Input } from "@/components/ui/Input";

const meta: Meta<typeof Field> = {
  title: "Design System/Field",
  component: Field,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Field>;

export const Default: Story = {
  render: () => (
    <FieldGroup className="w-96">
      <Field>
        <FieldLabel htmlFor="slug">Server slug</FieldLabel>
        <Input id="slug" placeholder="petstore" />
        <FieldDescription>Lowercase, no spaces.</FieldDescription>
      </Field>
      <Field>
        <FieldLabel htmlFor="name">Display name</FieldLabel>
        <Input id="name" />
        <FieldError>Display name is required.</FieldError>
      </Field>
    </FieldGroup>
  ),
};
