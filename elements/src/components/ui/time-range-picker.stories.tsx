import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { TimeRangePicker, type TimeRange } from './time-range-picker'

const meta: Meta<typeof TimeRangePicker> = {
  title: 'UI/TimeRangePicker',
  component: TimeRangePicker,
  tags: ['autodocs'],
  parameters: {
    layout: 'centered',
  },
  decorators: [
    (Story) => (
      <div className="gram-elements bg-background text-foreground min-w-[400px] p-8">
        <Story />
      </div>
    ),
  ],
  argTypes: {
    size: {
      control: 'select',
      options: ['sm', 'default', 'lg'],
    },
    showLive: {
      control: 'boolean',
    },
    showInterpretation: {
      control: 'boolean',
    },
    enableLLMParsing: {
      control: 'boolean',
    },
    disabled: {
      control: 'boolean',
    },
  },
}

export default meta
type Story = StoryObj<typeof TimeRangePicker>

/**
 * Default time range picker with preset badges and calendar.
 */
export const Default: Story = {
  args: {
    placeholder: 'Enter time range...',
    showLive: true,
    showInterpretation: true,
  },
}

/**
 * Time range picker with timezone indicator.
 */
export const WithTimezone: Story = {
  args: {
    timezone: 'UTC-08:00',
    placeholder: 'Enter time range...',
  },
}

/**
 * Small size variant.
 */
export const Small: Story = {
  args: {
    size: 'sm',
    placeholder: 'Enter time range...',
  },
}

/**
 * Large size variant.
 */
export const Large: Story = {
  args: {
    size: 'lg',
    placeholder: 'Enter time range...',
  },
}

/**
 * Without the LIVE mode toggle.
 */
export const WithoutLive: Story = {
  args: {
    showLive: false,
    placeholder: 'Enter time range...',
  },
}

/**
 * Without interpretation display.
 */
export const WithoutInterpretation: Story = {
  args: {
    showInterpretation: false,
    placeholder: 'Enter time range...',
  },
}

/**
 * Disabled state.
 */
export const Disabled: Story = {
  args: {
    disabled: true,
    placeholder: 'Enter time range...',
  },
}

/**
 * Custom presets configuration.
 */
export const CustomPresets: Story = {
  args: {
    presets: [
      { label: '5m', value: '5m', duration: 5 * 60 * 1000 },
      { label: '30m', value: '30m', duration: 30 * 60 * 1000 },
      { label: '2h', value: '2h', duration: 2 * 60 * 60 * 1000 },
      { label: '12h', value: '12h', duration: 12 * 60 * 60 * 1000 },
    ],
    placeholder: 'Enter time range...',
  },
}

/**
 * Controlled component example with external state.
 */
export const Controlled: Story = {
  render: () => {
    const [value, setValue] = useState<TimeRange | undefined>(undefined)

    return (
      <div className="space-y-4">
        <TimeRangePicker
          value={value}
          onChange={(range) => {
            setValue(range)
            console.log('Time range changed:', range)
          }}
          placeholder="Select a time range"
        />
        <div className="text-muted-foreground bg-secondary rounded-md p-2 text-xs">
          <strong>Current value:</strong>
          {value ? (
            <pre className="mt-1">
              {JSON.stringify(
                {
                  start: value.start.toISOString(),
                  end: value.end.toISOString(),
                },
                null,
                2
              )}
            </pre>
          ) : (
            <span className="ml-2">None selected</span>
          )}
        </div>
      </div>
    )
  },
}

/**
 * Full Datadog-style configuration with timezone and all features.
 */
export const DatadogStyle: Story = {
  args: {
    timezone: 'UTC-08:00',
    showLive: true,
    showInterpretation: true,
    presets: [
      { label: '15m', value: '15m', duration: 15 * 60 * 1000 },
      { label: '1h', value: '1h', duration: 60 * 60 * 1000 },
      { label: '4h', value: '4h', duration: 4 * 60 * 60 * 1000 },
      { label: '24h', value: '24h', duration: 24 * 60 * 60 * 1000 },
      { label: '7d', value: '7d', duration: 7 * 24 * 60 * 60 * 1000 },
    ],
    placeholder: 'e.g., "3 days ago" or "last week"',
  },
}

/**
 * Showing parsing in action. Type natural language like:
 * - "yesterday"
 * - "3 days ago"
 * - "last week"
 * - "15m"
 */
export const NaturalLanguageParsing: Story = {
  args: {
    placeholder: 'Try: "yesterday", "3 days ago", "last week"',
    showInterpretation: true,
  },
}

/**
 * With LLM parsing enabled (requires API configuration).
 * Note: In this story, LLM calls will fail without proper API setup.
 */
export const WithLLMParsing: Story = {
  args: {
    enableLLMParsing: true,
    apiUrl: 'https://app.getgram.ai',
    placeholder: 'Try: "yesterday around 5pm"',
    showInterpretation: true,
  },
}
