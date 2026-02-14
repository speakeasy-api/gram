import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import {
  TimeRangePicker,
  type TimeRange,
  type DateRangePreset,
} from './time-range-picker'

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
    showLive: {
      control: 'boolean',
    },
    disabled: {
      control: 'boolean',
    },
    timezone: {
      control: 'text',
    },
  },
}

export default meta
type Story = StoryObj<typeof TimeRangePicker>

/**
 * Default time range picker with preset badges and calendar.
 * Supports natural language input with AI parsing.
 */
export const Default: Story = {
  render: () => {
    const [preset, setPreset] = useState<DateRangePreset | null>('7d')
    const [customRange, setCustomRange] = useState<TimeRange | null>(null)

    return (
      <TimeRangePicker
        preset={customRange ? null : preset}
        customRange={customRange}
        onPresetChange={(p) => {
          setPreset(p)
          setCustomRange(null)
        }}
        onCustomRangeChange={(from, to, label) => {
          setCustomRange({ from, to })
          setPreset(null)
          console.log('Custom range:', { from, to, label })
        }}
        onClearCustomRange={() => {
          setCustomRange(null)
          setPreset('7d')
        }}
      />
    )
  },
}

/**
 * Time range picker with timezone indicator.
 */
export const WithTimezone: Story = {
  render: () => {
    const [preset, setPreset] = useState<DateRangePreset | null>('30d')
    const [customRange, setCustomRange] = useState<TimeRange | null>(null)

    return (
      <TimeRangePicker
        preset={customRange ? null : preset}
        customRange={customRange}
        onPresetChange={(p) => {
          setPreset(p)
          setCustomRange(null)
        }}
        onCustomRangeChange={(from, to) => {
          setCustomRange({ from, to })
          setPreset(null)
        }}
        onClearCustomRange={() => {
          setCustomRange(null)
          setPreset('30d')
        }}
        timezone="UTC-08:00"
      />
    )
  },
}

/**
 * With LIVE mode toggle enabled.
 */
export const WithLiveMode: Story = {
  render: () => {
    const [preset, setPreset] = useState<DateRangePreset | null>('15m')
    const [customRange, setCustomRange] = useState<TimeRange | null>(null)
    const [isLive, setIsLive] = useState(true)

    return (
      <TimeRangePicker
        preset={customRange ? null : preset}
        customRange={customRange}
        onPresetChange={(p) => {
          setPreset(p)
          setCustomRange(null)
        }}
        onCustomRangeChange={(from, to) => {
          setCustomRange({ from, to })
          setPreset(null)
        }}
        onClearCustomRange={() => {
          setCustomRange(null)
          setPreset('15m')
        }}
        showLive
        isLive={isLive}
        onLiveChange={setIsLive}
      />
    )
  },
}

/**
 * Disabled state.
 */
export const Disabled: Story = {
  args: {
    preset: '7d',
    disabled: true,
  },
}

/**
 * Full Datadog-style configuration with all features.
 * Type natural language like "3 days ago", "last Wednesday", "past 2 weeks".
 */
export const DatadogStyle: Story = {
  render: () => {
    const [preset, setPreset] = useState<DateRangePreset | null>('7d')
    const [customRange, setCustomRange] = useState<TimeRange | null>(null)
    const [customLabel, setCustomLabel] = useState<string | null>(null)
    const [isLive, setIsLive] = useState(false)

    return (
      <div className="space-y-4">
        <TimeRangePicker
          preset={customRange ? null : preset}
          customRange={customRange}
          customRangeLabel={customLabel}
          onPresetChange={(p) => {
            setPreset(p)
            setCustomRange(null)
            setCustomLabel(null)
          }}
          onCustomRangeChange={(from, to, label) => {
            setCustomRange({ from, to })
            setPreset(null)
            setCustomLabel(label || null)
          }}
          onClearCustomRange={() => {
            setCustomRange(null)
            setPreset('7d')
            setCustomLabel(null)
          }}
          showLive
          isLive={isLive}
          onLiveChange={setIsLive}
          timezone="UTC-08:00"
        />
        <div className="text-muted-foreground bg-muted rounded-md p-3 text-xs">
          <strong>Current state:</strong>
          <pre className="mt-1 overflow-auto">
            {JSON.stringify(
              {
                preset,
                customRange: customRange
                  ? {
                      from: customRange.from.toISOString(),
                      to: customRange.to.toISOString(),
                    }
                  : null,
                customLabel,
                isLive,
              },
              null,
              2
            )}
          </pre>
        </div>
      </div>
    )
  },
}

/**
 * Natural language parsing demo.
 * Type things like:
 * - "yesterday"
 * - "3 days ago"
 * - "last Wednesday"
 * - "past 2 weeks"
 * - "January 2024"
 */
export const NaturalLanguageParsing: Story = {
  render: () => {
    const [preset, setPreset] = useState<DateRangePreset | null>('30d')
    const [customRange, setCustomRange] = useState<TimeRange | null>(null)
    const [customLabel, setCustomLabel] = useState<string | null>(null)

    return (
      <div className="space-y-4">
        <p className="text-muted-foreground text-sm">
          Try typing: "yesterday", "3 days ago", "last Wednesday", "January"
        </p>
        <TimeRangePicker
          preset={customRange ? null : preset}
          customRange={customRange}
          customRangeLabel={customLabel}
          onPresetChange={(p) => {
            setPreset(p)
            setCustomRange(null)
            setCustomLabel(null)
          }}
          onCustomRangeChange={(from, to, label) => {
            setCustomRange({ from, to })
            setPreset(null)
            setCustomLabel(label || null)
            console.log('AI parsed:', { from, to, label })
          }}
          onClearCustomRange={() => {
            setCustomRange(null)
            setPreset('30d')
            setCustomLabel(null)
          }}
        />
      </div>
    )
  },
}
