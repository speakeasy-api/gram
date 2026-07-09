import type { Meta, StoryObj } from "@storybook/react-vite";
import { faker } from "@faker-js/faker";
import {
  Database,
  GitBranch,
  Globe,
  Server,
  MessageSquare,
  Zap,
} from "lucide-react";
import { MultiSelect } from "@/components/ui/multi-select";
import type {
  MultiSelectGroup,
  MultiSelectOption,
} from "@/components/ui/multi-select";

faker.seed(42);

const meta: Meta<typeof MultiSelect> = {
  title: "UI/MultiSelect",
  component: MultiSelect,
  parameters: {
    layout: "padded",
  },
};

export default meta;

type Story = StoryObj<typeof MultiSelect>;

const SERVER_OPTIONS: MultiSelectOption[] = [
  { label: "GitHub", value: "github", icon: GitBranch },
  { label: "MessageSquare", value: "slack", icon: MessageSquare },
  { label: "PostgreSQL", value: "postgres", icon: Database },
  { label: "Internal API", value: "internal-api", icon: Server },
  { label: "Public Web", value: "public-web", icon: Globe },
  { label: "Zapier", value: "zapier", icon: Zap },
  { label: faker.company.name(), value: "acme-corp" },
  { label: faker.company.name(), value: "globex" },
];

const GROUPED_OPTIONS: MultiSelectGroup[] = [
  {
    heading: "Hosted servers",
    options: [
      { label: "Internal API", value: "internal-api", icon: Server },
      { label: "PostgreSQL", value: "postgres", icon: Database },
    ],
  },
  {
    heading: "Catalog servers",
    options: [
      { label: "GitHub", value: "github", icon: GitBranch },
      { label: "MessageSquare", value: "slack", icon: MessageSquare },
      { label: "Zapier", value: "zapier", icon: Zap },
    ],
  },
];

export const Basic: Story = {
  render: () => (
    <MultiSelect
      options={SERVER_OPTIONS}
      onValueChange={() => {}}
      placeholder="Select servers..."
      autoSize
    />
  ),
};

export const Preselected: Story = {
  render: () => (
    <MultiSelect
      options={SERVER_OPTIONS}
      defaultValue={["github", "slack", "postgres"]}
      onValueChange={() => {}}
      placeholder="Select servers..."
      autoSize
    />
  ),
};

export const Grouped: Story = {
  render: () => (
    <MultiSelect
      options={GROUPED_OPTIONS}
      defaultValue={["internal-api"]}
      onValueChange={() => {}}
      placeholder="Select servers..."
      autoSize
    />
  ),
};

export const Disabled: Story = {
  render: () => (
    <MultiSelect
      options={SERVER_OPTIONS}
      defaultValue={["github", "postgres"]}
      onValueChange={() => {}}
      placeholder="Select servers..."
      disabled
      autoSize
    />
  ),
};
