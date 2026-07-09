import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/moonshine";

const meta: Meta<typeof Field> = {
  title: "UI/Field",
  component: Field,
  parameters: {
    layout: "padded",
  },
};

export default meta;

type Story = StoryObj<typeof Field>;

const NAME_MAX_LENGTH = 40;

function BrandingFormExample() {
  const [name, setName] = useState("My MCP server");

  return (
    <FieldGroup className="max-w-md gap-6">
      <Field>
        <FieldLabel htmlFor="story-display-name">Display Name</FieldLabel>
        <Input
          id="story-display-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="My MCP server"
          maxLength={NAME_MAX_LENGTH}
        />
        <FieldDescription className="pl-1 text-xs">
          {name.length} of {NAME_MAX_LENGTH} characters used
        </FieldDescription>
      </Field>

      <Field data-invalid>
        <FieldLabel htmlFor="story-slug">Slug</FieldLabel>
        <Input
          id="story-slug"
          defaultValue="my mcp server!"
          aria-invalid
          error
        />
        <FieldError>
          Slugs may only contain lowercase letters, numbers, and hyphens.
        </FieldError>
      </Field>
    </FieldGroup>
  );
}

export const RealisticForm: Story = {
  render: () => <BrandingFormExample />,
};

export const Basic: Story = {
  render: () => (
    <Field className="max-w-md">
      <FieldLabel htmlFor="story-basic">Server Name</FieldLabel>
      <Input id="story-basic" placeholder="Production API" />
      <FieldDescription>Shown to users on the install page.</FieldDescription>
    </Field>
  ),
};

export const WithError: Story = {
  render: () => (
    <Field className="max-w-md" data-invalid>
      <FieldLabel htmlFor="story-error">Webhook URL</FieldLabel>
      <Input id="story-error" defaultValue="not-a-url" aria-invalid error />
      <FieldError>Enter a valid https:// URL.</FieldError>
    </Field>
  ),
};

export const OptionalLabel: Story = {
  render: () => (
    <Field className="max-w-md">
      <FieldLabel htmlFor="story-optional" optional>
        Description
      </FieldLabel>
      <Input
        id="story-optional"
        placeholder="Used to identify your MCP server"
      />
    </Field>
  ),
};

export const HorizontalOrientation: Story = {
  render: () => (
    <Field orientation="horizontal" className="max-w-md">
      <FieldLabel htmlFor="story-horizontal">Enabled</FieldLabel>
      <Checkbox id="story-horizontal" />
    </Field>
  ),
};

export const Group: Story = {
  render: () => (
    <FieldGroup className="max-w-md">
      <Field>
        <FieldLabel htmlFor="story-group-name">Name</FieldLabel>
        <Input id="story-group-name" placeholder="Acme MCP" />
      </Field>
      <Field>
        <FieldLabel htmlFor="story-group-desc">Description</FieldLabel>
        <Input
          id="story-group-desc"
          placeholder="Used to identify your MCP server"
        />
        <FieldDescription>
          Optional — shown in the catalog listing.
        </FieldDescription>
      </Field>
      <Field data-invalid>
        <FieldLabel htmlFor="story-group-limit">Rate limit</FieldLabel>
        <Input
          id="story-group-limit"
          type="number"
          defaultValue={-1}
          aria-invalid
          error
        />
        <FieldError errors={[{ message: "Must be a positive number." }]} />
      </Field>
    </FieldGroup>
  ),
};
