import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { Search, Zap, ChevronDown, X } from 'lucide-react'

import { cn } from '@/lib/utils'
import { useTimeRange, type UseTimeRangeOptions } from '@/hooks/useTimeRange'
import type { TimeRange, TimeRangePreset } from '@/lib/time-parser'
import { DEFAULT_PRESETS, formatTimeRange } from '@/lib/time-parser'
import { Popover, PopoverContent, PopoverTrigger } from './popover'
import { Calendar } from './calendar'

const timeRangePickerVariants = cva('flex flex-col gap-2 w-full', {
  variants: {
    size: {
      sm: 'text-xs',
      default: 'text-sm',
      lg: 'text-base',
    },
  },
  defaultVariants: {
    size: 'default',
  },
})

export type { TimeRange, TimeRangePreset }

export interface TimeRangePickerProps
  extends
    Omit<React.HTMLAttributes<HTMLDivElement>, 'onChange'>,
    VariantProps<typeof timeRangePickerVariants> {
  /** Current time range value */
  value?: TimeRange
  /** Called when time range changes */
  onChange?: (range: TimeRange) => void
  /** Timezone to display (e.g., "UTC-08:00") */
  timezone?: string
  /** Show LIVE mode toggle */
  showLive?: boolean
  /** Enable LLM parsing for complex inputs */
  enableLLMParsing?: boolean
  /** API URL for LLM parsing */
  apiUrl?: string
  /** Auth headers for LLM parsing */
  authHeaders?: Record<string, string>
  /** Custom presets (defaults provided) */
  presets?: TimeRangePreset[]
  /** Placeholder text */
  placeholder?: string
  /** Show interpreted range description */
  showInterpretation?: boolean
  /** Disable the component */
  disabled?: boolean
}

const TimeRangePicker = React.forwardRef<HTMLDivElement, TimeRangePickerProps>(
  (
    {
      className,
      size,
      value,
      onChange,
      timezone,
      showLive = true,
      enableLLMParsing = false,
      apiUrl,
      authHeaders,
      presets = DEFAULT_PRESETS,
      placeholder = 'Enter time range...',
      showInterpretation = true,
      disabled = false,
      ...props
    },
    ref
  ) => {
    const [isPopoverOpen, setIsPopoverOpen] = React.useState(false)

    const hookOptions: UseTimeRangeOptions = {
      initialValue: value,
      enableLLMParsing,
      apiUrl,
      authHeaders,
      timezone,
      onChange,
    }

    const {
      inputValue,
      parsedRange,
      interpretation,
      isLive,
      isParsing,
      parseError,
      setInputValue,
      setLive,
      selectPreset,
      selectDateRange,
      clear,
    } = useTimeRange(hookOptions)

    // Sync external value changes
    React.useEffect(() => {
      if (value && parsedRange) {
        const sameStart = value.start.getTime() === parsedRange.start.getTime()
        const sameEnd = value.end.getTime() === parsedRange.end.getTime()
        if (!sameStart || !sameEnd) {
          // External value changed, but we don't reset internal state
          // This allows controlled usage while maintaining internal state
        }
      }
    }, [value, parsedRange])

    const handlePresetClick = (preset: TimeRangePreset) => {
      selectPreset(preset)
      setIsPopoverOpen(false)
    }

    const handleCalendarSelect = (range: { start: Date; end: Date | null }) => {
      if (range.start && range.end) {
        selectDateRange(range)
        setIsPopoverOpen(false)
      }
    }

    const handleLiveToggle = () => {
      setLive(!isLive)
    }

    const handleClear = () => {
      clear()
    }

    return (
      <div
        ref={ref}
        data-slot="time-range-picker"
        className={cn(timeRangePickerVariants({ size, className }))}
        {...props}
      >
        {/* Top row: timezone, presets, live toggle */}
        <div className="flex flex-wrap items-center gap-2">
          {/* Timezone indicator */}
          {timezone && (
            <button
              type="button"
              disabled={disabled}
              className={cn(
                'inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs',
                'bg-secondary text-secondary-foreground',
                'hover:bg-secondary/80 transition-colors',
                disabled && 'cursor-not-allowed opacity-50'
              )}
            >
              {timezone}
              <ChevronDown className="h-3 w-3" />
            </button>
          )}

          {/* Preset badges */}
          <div className="flex gap-1">
            {presets.map((preset) => (
              <button
                key={preset.value}
                type="button"
                disabled={disabled}
                onClick={() => handlePresetClick(preset)}
                className={cn(
                  'inline-flex items-center justify-center rounded-full px-2 py-0.5 text-xs font-medium',
                  'bg-secondary text-secondary-foreground',
                  'hover:bg-secondary/80 transition-colors',
                  'focus-visible:ring-ring/50 focus-visible:ring-[3px] focus-visible:outline-none',
                  disabled && 'cursor-not-allowed opacity-50'
                )}
              >
                {preset.label}
              </button>
            ))}
          </div>

          {/* Spacer */}
          <div className="flex-1" />

          {/* Live toggle */}
          {showLive && (
            <button
              type="button"
              disabled={disabled}
              onClick={handleLiveToggle}
              className={cn(
                'inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors',
                'focus-visible:ring-ring/50 focus-visible:ring-[3px] focus-visible:outline-none',
                isLive
                  ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-100'
                  : 'bg-secondary text-secondary-foreground hover:bg-secondary/80',
                disabled && 'cursor-not-allowed opacity-50'
              )}
            >
              <Zap className={cn('h-3 w-3', isLive && 'fill-current')} />
              LIVE
            </button>
          )}
        </div>

        {/* Input row */}
        <Popover open={isPopoverOpen} onOpenChange={setIsPopoverOpen}>
          <PopoverTrigger asChild disabled={disabled}>
            <div
              className={cn(
                'relative flex items-center',
                'bg-background border-input rounded-md border',
                'focus-within:border-ring focus-within:ring-ring/50 focus-within:ring-[3px]',
                'transition-[border-color,box-shadow]',
                disabled && 'cursor-not-allowed opacity-50'
              )}
            >
              <input
                type="text"
                value={inputValue}
                onChange={(e) => setInputValue(e.target.value)}
                placeholder={placeholder}
                disabled={disabled}
                className={cn(
                  'flex-1 bg-transparent px-3 py-2 outline-none',
                  'placeholder:text-muted-foreground',
                  'disabled:cursor-not-allowed',
                  size === 'sm' && 'h-8 text-xs',
                  size === 'lg' && 'h-11 text-base',
                  (!size || size === 'default') && 'h-9 text-sm'
                )}
                onFocus={() => setIsPopoverOpen(true)}
              />

              {/* Clear button */}
              {(inputValue || parsedRange) && !disabled && (
                <button
                  type="button"
                  onClick={handleClear}
                  className="hover:bg-accent mr-1 rounded p-1 transition-colors"
                  aria-label="Clear"
                >
                  <X className="text-muted-foreground h-4 w-4" />
                </button>
              )}

              {/* Search icon */}
              <div className="pr-3">
                {isParsing ? (
                  <div className="border-primary h-4 w-4 animate-spin rounded-full border-2 border-t-transparent" />
                ) : (
                  <Search className="text-muted-foreground h-4 w-4" />
                )}
              </div>
            </div>
          </PopoverTrigger>

          <PopoverContent className="w-auto p-0" align="start">
            <div className="flex flex-col">
              {/* Quick suggestions */}
              <div className="border-b p-3">
                <p className="text-muted-foreground mb-2 text-xs">
                  Try typing:
                </p>
                <div className="flex flex-wrap gap-1">
                  {['yesterday', '3 days ago', 'last week', 'this month'].map(
                    (suggestion) => (
                      <button
                        key={suggestion}
                        type="button"
                        onClick={() => {
                          setInputValue(suggestion)
                          setIsPopoverOpen(false)
                        }}
                        className={cn(
                          'rounded-md px-2 py-1 text-xs',
                          'bg-secondary text-secondary-foreground',
                          'hover:bg-secondary/80 transition-colors'
                        )}
                      >
                        {suggestion}
                      </button>
                    )
                  )}
                </div>
              </div>

              {/* Calendar */}
              <div className="border-b">
                <Calendar
                  mode="range"
                  selected={{
                    start: parsedRange?.start ?? null,
                    end: parsedRange?.end ?? null,
                  }}
                  onSelect={handleCalendarSelect}
                  maxDate={new Date()}
                />
              </div>

              {/* Selected range display */}
              {parsedRange && (
                <div className="text-muted-foreground p-3 text-xs">
                  Selected: {formatTimeRange(parsedRange, { timezone })}
                </div>
              )}
            </div>
          </PopoverContent>
        </Popover>

        {/* Interpretation row */}
        {showInterpretation && (
          <div className="text-muted-foreground min-h-[1.25rem] text-xs">
            {parseError ? (
              <span className="text-destructive">{parseError}</span>
            ) : parsedRange ? (
              <span>{interpretation}</span>
            ) : null}
          </div>
        )}
      </div>
    )
  }
)
TimeRangePicker.displayName = 'TimeRangePicker'

// eslint-disable-next-line react-refresh/only-export-components
export { TimeRangePicker, timeRangePickerVariants }
