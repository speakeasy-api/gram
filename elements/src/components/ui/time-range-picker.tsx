/* eslint-disable react-refresh/only-export-components */
import * as React from 'react'
import { CalendarIcon, ChevronDown, Zap } from 'lucide-react'
import { generateObject } from 'ai'
import { createOpenRouter } from '@openrouter/ai-sdk-provider'
import { z } from 'zod'

import { cn } from '@/lib/utils'
import { Popover, PopoverContent, PopoverTrigger } from './popover'
import { Calendar } from './calendar'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface TimeRange {
  from: Date
  to: Date
}

export type DateRangePreset =
  | '15m'
  | '1h'
  | '4h'
  | '1d'
  | '2d'
  | '3d'
  | '7d'
  | '15d'
  | '30d'
  | '90d'

export interface TimeRangePreset {
  label: string
  shortLabel: string
  value: DateRangePreset
  getRange: () => TimeRange
}

// ---------------------------------------------------------------------------
// Date Utilities (no external dependencies)
// ---------------------------------------------------------------------------

function formatDate(date: Date, pattern: 'short' | 'medium' = 'short'): string {
  if (pattern === 'short') {
    return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
  }
  return date.toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  })
}

// ---------------------------------------------------------------------------
// Presets Configuration
// ---------------------------------------------------------------------------

export const PRESETS: TimeRangePreset[] = [
  {
    label: 'Past 15 Minutes',
    shortLabel: '15m',
    value: '15m',
    getRange: () => ({
      from: new Date(Date.now() - 15 * 60 * 1000),
      to: new Date(),
    }),
  },
  {
    label: 'Past 1 Hour',
    shortLabel: '1h',
    value: '1h',
    getRange: () => ({
      from: new Date(Date.now() - 60 * 60 * 1000),
      to: new Date(),
    }),
  },
  {
    label: 'Past 4 Hours',
    shortLabel: '4h',
    value: '4h',
    getRange: () => ({
      from: new Date(Date.now() - 4 * 60 * 60 * 1000),
      to: new Date(),
    }),
  },
  {
    label: 'Past 1 Day',
    shortLabel: '1d',
    value: '1d',
    getRange: () => ({
      from: new Date(Date.now() - 24 * 60 * 60 * 1000),
      to: new Date(),
    }),
  },
  {
    label: 'Past 2 Days',
    shortLabel: '2d',
    value: '2d',
    getRange: () => ({
      from: new Date(Date.now() - 2 * 24 * 60 * 60 * 1000),
      to: new Date(),
    }),
  },
  {
    label: 'Past 3 Days',
    shortLabel: '3d',
    value: '3d',
    getRange: () => ({
      from: new Date(Date.now() - 3 * 24 * 60 * 60 * 1000),
      to: new Date(),
    }),
  },
  {
    label: 'Past 7 Days',
    shortLabel: '1w',
    value: '7d',
    getRange: () => ({
      from: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000),
      to: new Date(),
    }),
  },
  {
    label: 'Past 15 Days',
    shortLabel: '15d',
    value: '15d',
    getRange: () => ({
      from: new Date(Date.now() - 15 * 24 * 60 * 60 * 1000),
      to: new Date(),
    }),
  },
  {
    label: 'Past 1 Month',
    shortLabel: '1mo',
    value: '30d',
    getRange: () => ({
      from: new Date(Date.now() - 30 * 24 * 60 * 60 * 1000),
      to: new Date(),
    }),
  },
  {
    label: 'Past 3 Months',
    shortLabel: '3mo',
    value: '90d',
    getRange: () => ({
      from: new Date(Date.now() - 90 * 24 * 60 * 60 * 1000),
      to: new Date(),
    }),
  },
]

// Badge width class - shared between trigger and dropdown for alignment
const BADGE_WIDTH = 'min-w-10'

export function getPresetRange(preset: DateRangePreset): TimeRange {
  const p = PRESETS.find((p) => p.value === preset)
  return p ? p.getRange() : PRESETS[5].getRange() // Default to 3d
}

function getPresetByValue(value: DateRangePreset): TimeRangePreset | undefined {
  return PRESETS.find((p) => p.value === value)
}

// ---------------------------------------------------------------------------
// AI Time Range Parser
// ---------------------------------------------------------------------------

type ParseResult =
  | { type: 'preset'; preset: DateRangePreset }
  | { type: 'custom'; range: TimeRange; label?: string }
  | null

const timeRangeSchema = z.object({
  from: z.string().describe('ISO8601 start date/time'),
  to: z.string().describe('ISO8601 end date/time'),
  label: z.string().describe('Short semantic label for the range'),
})

const TIME_RANGE_MODEL = 'openai/gpt-4o-mini'

/**
 * Parse an ISO date string as a local date (ignoring timezone).
 * This prevents timezone shifts when the AI returns dates like "2026-02-09T00:00:00Z"
 * which would otherwise display as Feb 8 in US timezones.
 */
function parseAsLocalDate(isoString: string): Date {
  // Try to extract just the date part and create a local date
  const dateMatch = isoString.match(/^(\d{4})-(\d{2})-(\d{2})/)
  if (dateMatch) {
    const [, year, month, day] = dateMatch
    // Check if there's a time component
    const timeMatch = isoString.match(/T(\d{2}):(\d{2}):?(\d{2})?/)
    if (timeMatch) {
      const [, hours, minutes, seconds = '0'] = timeMatch
      return new Date(
        parseInt(year),
        parseInt(month) - 1,
        parseInt(day),
        parseInt(hours),
        parseInt(minutes),
        parseInt(seconds)
      )
    }
    // Date only - use start of day local time
    return new Date(parseInt(year), parseInt(month) - 1, parseInt(day))
  }
  // Fallback to standard parsing
  return new Date(isoString)
}

async function parseWithAI(
  input: string,
  apiUrl: string,
  projectSlug?: string
): Promise<ParseResult> {
  try {
    const now = new Date()

    // Create OpenRouter provider without X-Gram-Source header (so usage is billed)
    const headers: Record<string, string> = {}
    if (projectSlug) {
      headers['Gram-Project'] = projectSlug
    }

    const openRouter = createOpenRouter({
      baseURL: apiUrl,
      apiKey: 'unused',
      headers,
      fetch: (url, init) =>
        fetch(url, {
          ...init,
          credentials: 'include',
        }),
    })

    const model = openRouter.chat(TIME_RANGE_MODEL)

    const result = await generateObject({
      model,
      schema: timeRangeSchema,
      prompt: `You are a time range parser for an analytics dashboard. Parse natural language into a PAST time range.
Current time: ${now.toISOString()}

KEY RULES:
- "X days ago" = THE WHOLE DAY (from: start 00:00, to: end 23:59:59)
- "X months ago" = THE WHOLE MONTH (from: 1st 00:00, to: last day 23:59:59)
- "X years ago" = THE WHOLE YEAR (from: Jan 1, to: Dec 31)
- "past X days" = RANGE from X days ago to now
- "last wednesday" etc = that specific day (whole day)
- IMPORTANT: Return dates WITHOUT timezone suffix (no "Z"). Use format like "2026-02-09T00:00:00" not "2026-02-09T00:00:00Z"

LABEL RULES - use semantic labels:
- Duration presets: "15m", "1h", "4h", "1d", "2d", "3d", "7d", "15d", "30d"
- Single day: use 3-letter day name "Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"
- Whole month: use 3-letter month name "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"
- Whole year: use the year "2024", "2025"
- Date range: use "M/D-M/D" format like "1/5-1/10" or "12/1-12/15"

Examples:
- "4 days ago" -> label: "Mon" (or whatever day it was)
- "2 months ago" -> label: "Dec" (or whatever month)
- "1 year ago" -> label: "2025" (or whatever year)
- "past 3 days" -> label: "3d"
- "last wednesday" -> label: "Wed"
- "jan 5 to jan 10" -> label: "1/5-1/10"

User input: ${input}`,
    })

    const parsed = result.object
    // Parse dates as local to avoid timezone shifts
    const from = parseAsLocalDate(parsed.from)
    const to = parseAsLocalDate(parsed.to)

    if (isNaN(from.getTime()) || isNaN(to.getTime())) {
      return null
    }

    // Normalize labels like "1w" -> "7d", "2w" -> "14d"
    let normalizedLabel = parsed.label
    if (normalizedLabel === '1w') normalizedLabel = '7d'
    if (normalizedLabel === '2w') normalizedLabel = '14d'
    if (normalizedLabel === '1mo') normalizedLabel = '30d'
    if (normalizedLabel === '3mo') normalizedLabel = '90d'

    const matchedPreset = PRESETS.find((p) => p.value === normalizedLabel)
    if (matchedPreset) {
      return { type: 'preset', preset: matchedPreset.value }
    }

    // Use the semantic label from AI (e.g., "Mon", "Jan", "2024", "1/5-1/10")
    return { type: 'custom', range: { from, to }, label: parsed.label }
  } catch {
    return null
  }
}

// ---------------------------------------------------------------------------
// TimeRangePicker Component (Datadog Style)
// ---------------------------------------------------------------------------

export interface TimeRangePickerProps {
  /** Current preset value */
  preset?: DateRangePreset | null
  /** Current custom range */
  customRange?: TimeRange | null
  /** Called when a preset is selected */
  onPresetChange?: (preset: DateRangePreset) => void
  /** Called when a custom range is selected */
  onCustomRangeChange?: (from: Date, to: Date, label?: string) => void
  /** Called to clear custom range */
  onClearCustomRange?: () => void
  /** Initial label for custom range (from URL params) */
  customRangeLabel?: string | null
  /** Show LIVE mode option */
  showLive?: boolean
  /** Is LIVE mode active */
  isLive?: boolean
  /** Called when LIVE mode changes */
  onLiveChange?: (isLive: boolean) => void
  /** Disabled state */
  disabled?: boolean
  /** Timezone display (e.g., "UTC-08:00") */
  timezone?: string
  /** API URL for AI parsing (defaults to window.location.origin) */
  apiUrl?: string
  /** Project slug for API authentication */
  projectSlug?: string
}

function TimeRangePicker({
  preset,
  customRange,
  onPresetChange,
  onCustomRangeChange,
  onClearCustomRange,
  customRangeLabel: initialCustomLabel,
  showLive = false,
  isLive = false,
  onLiveChange,
  disabled = false,
  timezone,
  apiUrl,
  projectSlug,
}: TimeRangePickerProps) {
  const [isOpen, setIsOpen] = React.useState(false)
  const [showCalendar, setShowCalendar] = React.useState(false)
  const [inputValue, setInputValue] = React.useState('')
  const [isEditing, setIsEditing] = React.useState(false)
  const [isParsing, setIsParsing] = React.useState(false)
  const [customLabel, setCustomLabel] = React.useState<string | null>(
    initialCustomLabel || null
  )
  const inputRef = React.useRef<HTMLInputElement>(null)

  // Sync custom label from props (e.g., when URL changes)
  React.useEffect(() => {
    if (initialCustomLabel !== undefined) {
      setCustomLabel(initialCustomLabel || null)
    }
  }, [initialCustomLabel])

  const effectiveApiUrl =
    apiUrl || (typeof window !== 'undefined' ? window.location.origin : '')

  const handlePresetClick = (p: TimeRangePreset) => {
    onPresetChange?.(p.value)
    setCustomLabel(null)
    setIsOpen(false)
    setInputValue('')
  }

  const handleLiveClick = () => {
    onLiveChange?.(!isLive)
    if (!isLive) {
      // When enabling LIVE, also select a default short preset
      onPresetChange?.('15m')
    }
    setIsOpen(false)
  }

  const handleCalendarSelect = (range: { start: Date; end: Date | null }) => {
    if (range.start && range.end) {
      onCustomRangeChange?.(range.start, range.end)
      setCustomLabel(null) // Calendar selections don't have AI labels
      setIsOpen(false)
      setShowCalendar(false)
      setInputValue('')
    }
  }

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setInputValue(e.target.value)
  }

  const applyParseResult = (parsed: ParseResult) => {
    if (parsed) {
      if (parsed.type === 'preset') {
        onPresetChange?.(parsed.preset)
        setCustomLabel(null)
      } else {
        const label = parsed.label || undefined
        onCustomRangeChange?.(parsed.range.from, parsed.range.to, label)
        setCustomLabel(label || null)
      }
      setInputValue('')
      setIsOpen(false)
      setIsEditing(false)
      return true
    }
    return false
  }

  const handleInputKeyDown = async (
    e: React.KeyboardEvent<HTMLInputElement>
  ) => {
    if (e.key === 'Enter' && inputValue.trim() && !isParsing) {
      // Use AI to parse natural language input
      setIsParsing(true)
      try {
        const aiParsed = await parseWithAI(
          inputValue,
          effectiveApiUrl,
          projectSlug
        )
        applyParseResult(aiParsed)
      } finally {
        setIsParsing(false)
      }
    } else if (e.key === 'Escape') {
      setInputValue('')
      setIsEditing(false)
      setIsOpen(false)
    } else if (e.key === 'Backspace' && inputValue === '' && customRange) {
      // Clear custom range when backspacing on empty input
      e.preventDefault()
      onClearCustomRange?.()
    }
  }

  const handleInputClick = (e: React.MouseEvent) => {
    // Prevent the popover trigger from toggling closed
    e.stopPropagation()
    setIsEditing(true)
    setIsOpen(true)
  }

  const handleInputFocus = () => {
    setIsEditing(true)
    // Don't set isOpen here - let the click handler or popover manage it
  }

  const handleInputBlur = () => {
    // Delay to allow click events on dropdown items
    setTimeout(() => {
      if (!inputValue) {
        setIsEditing(false)
      }
    }, 150)
  }

  // Determine current range for display
  const currentRange = customRange ?? (preset ? getPresetRange(preset) : null)

  // Get short label for trigger badge
  const getShortLabel = () => {
    if (customRange) return customLabel || 'Custom'
    if (preset) {
      const presetObj = getPresetByValue(preset)
      return presetObj?.shortLabel ?? preset
    }
    return '7d'
  }

  // Get label text (preset label or custom range description)
  const getLabelText = () => {
    if (customRange) {
      return `${formatDate(customRange.from)} – ${formatDate(customRange.to)}`
    }
    if (preset) {
      const presetObj = getPresetByValue(preset)
      return presetObj?.label ?? 'Select time range'
    }
    return 'Select time range'
  }

  const handleOpenChange = (open: boolean) => {
    // If closing while editing, keep it open unless explicitly closed via selection
    if (!open && isEditing) {
      return
    }
    setIsOpen(open)
    if (open && inputRef.current) {
      // Focus input when opening
      setTimeout(() => inputRef.current?.focus(), 0)
    }
  }

  return (
    <Popover open={isOpen} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild disabled={disabled}>
        <div
          className={cn(
            'bg-background relative inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm transition-all outline-none',
            'border-border hover:border-border/80',
            disabled && 'cursor-not-allowed opacity-50',
            timezone && 'pt-4'
          )}
        >
          {/* Floating timezone legend */}
          {timezone && (
            <span className="bg-background text-muted-foreground absolute -top-2 left-3 px-1 text-xs">
              {timezone}
            </span>
          )}

          {/* Short badge */}
          <span
            className={cn(
              'inline-flex h-6 items-center justify-center rounded px-2 py-1 text-xs font-semibold',
              BADGE_WIDTH,
              isLive
                ? 'bg-green-500 text-white'
                : 'bg-muted text-muted-foreground'
            )}
          >
            {isParsing ? (
              <div className="h-3 w-3 animate-spin rounded-full border-2 border-current/30 border-t-current" />
            ) : (
              getShortLabel()
            )}
          </span>

          {/* Input field for natural language or display label */}
          <input
            ref={inputRef}
            type="text"
            value={isEditing ? inputValue : getLabelText()}
            onChange={handleInputChange}
            onClick={handleInputClick}
            onFocus={handleInputFocus}
            onBlur={handleInputBlur}
            onKeyDown={handleInputKeyDown}
            placeholder="e.g., 3 days ago, last week..."
            disabled={disabled}
            className={cn(
              'min-w-[140px] flex-1 bg-transparent outline-none',
              'placeholder:text-muted-foreground/60',
              !isEditing && 'cursor-pointer',
              disabled && 'cursor-not-allowed'
            )}
          />

          {/* Dropdown chevron */}
          <ChevronDown className="text-muted-foreground h-4 w-4 shrink-0" />
        </div>
      </PopoverTrigger>

      <PopoverContent
        className="w-64 p-0"
        align="start"
        onOpenAutoFocus={(e) => {
          // Prevent popover from stealing focus from the input
          e.preventDefault()
          inputRef.current?.focus()
        }}
      >
        <div className="flex flex-col">
          {/* Calendar view */}
          {showCalendar ? (
            <>
              <div className="border-border/50 flex items-center justify-between border-b px-3 py-2">
                <span className="text-muted-foreground text-xs font-medium">
                  Select date range
                </span>
                <button
                  type="button"
                  onClick={() => setShowCalendar(false)}
                  className="text-primary text-xs hover:underline"
                >
                  Back
                </button>
              </div>
              <Calendar
                selected={{
                  start: currentRange?.from ?? null,
                  end: currentRange?.to ?? null,
                }}
                onSelect={handleCalendarSelect}
                maxDate={new Date()}
              />
              {customRange && onClearCustomRange && (
                <div className="border-border/50 border-t p-2">
                  <button
                    type="button"
                    onClick={() => {
                      onClearCustomRange()
                      setShowCalendar(false)
                    }}
                    className="text-muted-foreground hover:text-foreground w-full text-xs transition-colors"
                  >
                    Clear custom range
                  </button>
                </div>
              )}
            </>
          ) : (
            /* Presets list */
            <div className="py-1">
              {/* LIVE option */}
              {showLive && (
                <button
                  type="button"
                  onClick={handleLiveClick}
                  className={cn(
                    'flex w-full items-center gap-3 px-3 py-2 text-sm transition-colors',
                    'hover:bg-muted',
                    isLive && 'bg-blue-500/10'
                  )}
                >
                  <span
                    className={cn(
                      'inline-flex items-center justify-center gap-1 rounded px-1.5 py-0.5 text-xs font-semibold',
                      BADGE_WIDTH,
                      isLive
                        ? 'bg-green-500 text-white'
                        : 'bg-green-100 text-green-700 dark:bg-green-900/50 dark:text-green-400'
                    )}
                  >
                    <Zap className="h-3 w-3" />
                    LIVE
                  </span>
                  <span className="text-foreground/80">15 Minutes</span>
                </button>
              )}

              {/* Preset options */}
              {PRESETS.map((p) => {
                const isSelected = preset === p.value && !customRange && !isLive
                return (
                  <button
                    key={p.value}
                    type="button"
                    onClick={() => handlePresetClick(p)}
                    className={cn(
                      'flex w-full items-center gap-3 px-3 py-2 text-sm transition-colors',
                      isSelected ? 'bg-blue-500 text-white' : 'hover:bg-muted'
                    )}
                  >
                    <span
                      className={cn(
                        'inline-flex items-center justify-center rounded px-1.5 py-0.5 text-xs font-semibold',
                        BADGE_WIDTH,
                        isSelected
                          ? 'bg-white/20 text-white'
                          : 'bg-muted text-muted-foreground'
                      )}
                    >
                      {p.shortLabel}
                    </span>
                    <span
                      className={
                        isSelected ? 'text-white' : 'text-foreground/80'
                      }
                    >
                      {p.label}
                    </span>
                  </button>
                )
              })}

              {/* Select from calendar */}
              <button
                type="button"
                onClick={() => setShowCalendar(true)}
                className={cn(
                  'flex w-full items-center gap-3 px-3 py-2 text-sm transition-colors',
                  customRange ? 'bg-blue-500 text-white' : 'hover:bg-muted'
                )}
              >
                <span
                  className={cn(
                    'inline-flex items-center justify-center rounded px-1.5 py-0.5',
                    BADGE_WIDTH,
                    customRange
                      ? 'bg-white/20 text-white'
                      : 'bg-muted text-muted-foreground'
                  )}
                >
                  <CalendarIcon className="h-4 w-4" />
                </span>
                <span
                  className={customRange ? 'text-white' : 'text-foreground/80'}
                >
                  Select from calendar...
                </span>
              </button>
            </div>
          )}
        </div>
      </PopoverContent>
    </Popover>
  )
}
TimeRangePicker.displayName = 'TimeRangePicker'

export { TimeRangePicker }
