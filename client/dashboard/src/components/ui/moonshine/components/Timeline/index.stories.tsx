import { Timeline } from ".";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { Button } from "../Button";
import {
  Code,
  Palette,
  Hammer,
  TestTube,
  Rocket,
  GitBranch,
  Database,
  CheckCircle,
  Clock,
  ChevronDown,
} from "lucide-react";

const meta: Meta<typeof Timeline> = {
  title: "Moonshine/Timeline",
  component: Timeline,
  tags: ["autodocs"],
  parameters: {
    layout: "centered",
  },
  argTypes: {},
};

export default meta;

type Story = StoryObj<typeof Timeline>;

// Composable API Stories
export const ComposableBasic: Story = {
  render: () => (
    <Timeline>
      <Timeline.Item icon={<Code />}>
        <Timeline.Content>
          <Timeline.Title>Planning</Timeline.Title>
          <Timeline.Description>
            Project requirements and technical planning.
          </Timeline.Description>
          <Timeline.Timestamp>Week 1</Timeline.Timestamp>
        </Timeline.Content>
      </Timeline.Item>

      <Timeline.Item icon={<Palette />}>
        <Timeline.Content>
          <Timeline.Title>Design</Timeline.Title>
          <Timeline.Description>
            UI/UX design and system architecture.
          </Timeline.Description>
          <Timeline.Timestamp>Week 2</Timeline.Timestamp>
        </Timeline.Content>
      </Timeline.Item>

      <Timeline.Item icon={<Hammer />}>
        <Timeline.Content>
          <Timeline.Title>Development</Timeline.Title>
          <Timeline.Description>
            Feature implementation and coding.
          </Timeline.Description>
          <Timeline.Timestamp>Week 3-4</Timeline.Timestamp>
        </Timeline.Content>
      </Timeline.Item>

      <Timeline.Item icon={<TestTube />}>
        <Timeline.Content>
          <Timeline.Title>Testing</Timeline.Title>
          <Timeline.Description>
            Quality assurance and bug fixes.
          </Timeline.Description>
          <Timeline.Timestamp>Week 5</Timeline.Timestamp>
        </Timeline.Content>
      </Timeline.Item>

      <Timeline.Item icon={<Rocket />}>
        <Timeline.Content>
          <Timeline.Title>Deployment</Timeline.Title>
          <Timeline.Description>
            Production release and monitoring.
          </Timeline.Description>
          <Timeline.Timestamp>Week 6</Timeline.Timestamp>
        </Timeline.Content>
      </Timeline.Item>
    </Timeline>
  ),
  parameters: {
    docs: {
      description: {
        story:
          "Composable Timeline API with full control over content structure.",
      },
    },
  },
};

export const ComposableCustomContent: Story = {
  render: () => (
    <Timeline>
      <Timeline.Item icon={<Database />}>
        <Timeline.Content>
          <Timeline.Title>Database Setup</Timeline.Title>
          <Timeline.Description>
            PostgreSQL instance configured with optimized settings.
            <div className="text-success-default mt-2 text-xs font-medium">
              ✓ Performance: 95ms avg query time
            </div>
          </Timeline.Description>
          <Timeline.Timestamp>Jan 15, 2024</Timeline.Timestamp>
        </Timeline.Content>
      </Timeline.Item>

      <Timeline.Item icon={<GitBranch />}>
        <Timeline.Content>
          <Timeline.Title>API Integration</Timeline.Title>
          <Timeline.Description>
            Connecting external services and third-party APIs.
            <div className="mt-2 rounded border border-warning-softest bg-warning-softest p-2">
              <div className="text-warning-default text-xs">
                ⚠️ Rate limit: 450/500 requests
              </div>
            </div>
          </Timeline.Description>
          <Timeline.Timestamp>Jan 17, 2024</Timeline.Timestamp>
        </Timeline.Content>
      </Timeline.Item>

      <Timeline.Item>
        <Timeline.Content>
          <Timeline.Title>Performance Testing</Timeline.Title>
          <Timeline.Description>
            Load testing and optimization.
            <div className="mt-2 text-xs text-muted">
              Estimated duration: 2-3 days
            </div>
          </Timeline.Description>
          <Timeline.Timestamp>Jan 20, 2024</Timeline.Timestamp>
        </Timeline.Content>
      </Timeline.Item>
    </Timeline>
  ),
  parameters: {
    docs: {
      description: {
        story:
          "Timeline with custom content including status indicators and rich formatting.",
      },
    },
  },
};

export const ComposableMinimal: Story = {
  render: () => (
    <Timeline>
      <Timeline.Item>
        <Timeline.Content>
          <Timeline.Title>Setup Complete</Timeline.Title>
        </Timeline.Content>
      </Timeline.Item>

      <Timeline.Item>
        <Timeline.Content>
          <Timeline.Title>In Progress</Timeline.Title>
        </Timeline.Content>
      </Timeline.Item>

      <Timeline.Item>
        <Timeline.Content>
          <Timeline.Title>Coming Next</Timeline.Title>
        </Timeline.Content>
      </Timeline.Item>
    </Timeline>
  ),
  parameters: {
    docs: {
      description: {
        story:
          "Minimal timeline with just titles, showcasing default icons and numbering.",
      },
    },
  },
};

export const ComposableWithTimestamps: Story = {
  render: () => (
    <Timeline>
      <Timeline.Item icon={<CheckCircle />}>
        <Timeline.Content>
          <Timeline.Title>Project Kickoff</Timeline.Title>
          <Timeline.Description>
            Initial meeting with stakeholders and team setup. Requirements
            gathering and project scope definition completed.
          </Timeline.Description>
          <Timeline.Timestamp>Jan 8, 2024 • 9:00 AM</Timeline.Timestamp>
        </Timeline.Content>
      </Timeline.Item>

      <Timeline.Item icon={<Palette />}>
        <Timeline.Content>
          <Timeline.Title>Design Review</Timeline.Title>
          <Timeline.Description>
            UI/UX mockups presented and approved. Design system components
            finalized and ready for development.
          </Timeline.Description>
          <Timeline.Timestamp>Jan 12, 2024 • 2:30 PM</Timeline.Timestamp>
        </Timeline.Content>
      </Timeline.Item>

      <Timeline.Item icon={<Code />}>
        <Timeline.Content>
          <Timeline.Title>Development Started</Timeline.Title>
          <Timeline.Description>
            Core functionality implementation began. Backend API endpoints
            created and frontend components initialized.
          </Timeline.Description>
          <Timeline.Timestamp>Jan 15, 2024 • 10:15 AM</Timeline.Timestamp>
        </Timeline.Content>
      </Timeline.Item>

      <Timeline.Item icon={<TestTube />}>
        <Timeline.Content>
          <Timeline.Title>Testing Phase</Timeline.Title>
          <Timeline.Description>
            Quality assurance testing in progress. Unit tests and integration
            tests running successfully.
          </Timeline.Description>
          <Timeline.Timestamp>Jan 22, 2024 • 4:45 PM</Timeline.Timestamp>
        </Timeline.Content>
      </Timeline.Item>

      <Timeline.Item icon={<Rocket />}>
        <Timeline.Content>
          <Timeline.Title>Production Deployment</Timeline.Title>
          <Timeline.Description>
            Application successfully deployed to production environment.
            Monitoring and performance metrics look good.
          </Timeline.Description>
          <Timeline.Timestamp>Jan 25, 2024 • 11:30 AM</Timeline.Timestamp>
        </Timeline.Content>
      </Timeline.Item>
    </Timeline>
  ),
  parameters: {
    docs: {
      description: {
        story:
          "Timeline showcasing the Timestamp component with detailed time information for each milestone.",
      },
    },
  },
};

export const ComposableWithSemanticHeadings: Story = {
  render: () => (
    <div>
      <div className="mb-4">
        <h1>Project Timeline</h1>
      </div>

      <Timeline>
        <Timeline.Item icon={<Code />}>
          <Timeline.Content>
            <Timeline.Title as="h2">Phase 1: Planning</Timeline.Title>
            <Timeline.Description>
              Initial project setup and requirements gathering.
            </Timeline.Description>
            <Timeline.Timestamp>Week 1</Timeline.Timestamp>
          </Timeline.Content>
        </Timeline.Item>

        <Timeline.Item icon={<Palette />}>
          <Timeline.Content>
            <Timeline.Title as="h2">Phase 2: Design</Timeline.Title>
            <Timeline.Description>
              UI/UX design and system architecture planning.
            </Timeline.Description>
            <Timeline.Timestamp>Week 2</Timeline.Timestamp>
          </Timeline.Content>
        </Timeline.Item>

        <Timeline.Item icon={<Hammer />}>
          <Timeline.Content>
            <Timeline.Title as="h2">Phase 3: Development</Timeline.Title>
            <Timeline.Description>
              Core feature implementation and testing.
            </Timeline.Description>
            <Timeline.Timestamp>Week 3-4</Timeline.Timestamp>
          </Timeline.Content>
        </Timeline.Item>
      </Timeline>
    </div>
  ),
  parameters: {
    docs: {
      description: {
        story:
          "Timeline with proper semantic heading structure using the `as` prop for accessibility.",
      },
    },
  },
};

export const ComposableWithHasMore: Story = {
  render: () => (
    <div className="space-y-4">
      <Timeline hasMore>
        <Timeline.Item icon={<CheckCircle />}>
          <Timeline.Content>
            <Timeline.Title>Account Created</Timeline.Title>
            <Timeline.Description>
              New user account successfully created and verified.
            </Timeline.Description>
            <Timeline.Timestamp>2 hours ago</Timeline.Timestamp>
          </Timeline.Content>
        </Timeline.Item>

        <Timeline.Item icon={<Database />}>
          <Timeline.Content>
            <Timeline.Title>Profile Setup</Timeline.Title>
            <Timeline.Description>
              User completed profile information and preferences.
            </Timeline.Description>
            <Timeline.Timestamp>1 hour ago</Timeline.Timestamp>
          </Timeline.Content>
        </Timeline.Item>

        <Timeline.Item icon={<Clock />}>
          <Timeline.Content>
            <Timeline.Title>First Login</Timeline.Title>
            <Timeline.Description>
              User successfully logged in and started exploring the platform.
            </Timeline.Description>
            <Timeline.Timestamp>30 minutes ago</Timeline.Timestamp>
          </Timeline.Content>
        </Timeline.Item>

        <Timeline.Item icon={<Code />}>
          <Timeline.Content>
            <Timeline.Title>API Key Generated</Timeline.Title>
            <Timeline.Description>
              Development API key created for integration testing.
            </Timeline.Description>
            <Timeline.Timestamp>15 minutes ago</Timeline.Timestamp>
          </Timeline.Content>
        </Timeline.Item>
      </Timeline>

      <div className="flex justify-center">
        <Button
          variant="secondary"
          size="sm"
          onClick={() => alert("Loading more timeline items...")}
        >
          <ChevronDown className="mr-2 h-4 w-4" />
          Load More
        </Button>
      </div>
    </div>
  ),
  parameters: {
    docs: {
      description: {
        story:
          'Timeline with hasMore prop enabled, extending the line past the last item with a "Load More" button to indicate more content can be loaded.',
      },
    },
  },
};
