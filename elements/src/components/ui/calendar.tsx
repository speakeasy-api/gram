import * as React from 'react'
import { ChevronLeft, ChevronRight } from 'lucide-react'

import { cn } from '@/lib/utils'

export interface CalendarProps {
  /** Selected date range */
  selected?: { start: Date | null; end: Date | null }
  /** Called when a date or range is selected */
  onSelect?: (range: { start: Date; end: Date | null }) => void
  /** Whether range selection is enabled */
  mode?: 'single' | 'range'
  /** Disable dates before this */
  minDate?: Date
  /** Disable dates after this */
  maxDate?: Date
  /** Additional className */
  className?: string
}

interface CalendarDay {
  date: Date
  isCurrentMonth: boolean
  isToday: boolean
  isSelected: boolean
  isInRange: boolean
  isRangeStart: boolean
  isRangeEnd: boolean
  isDisabled: boolean
}

const WEEKDAYS = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa']
const MONTHS = [
  'January',
  'February',
  'March',
  'April',
  'May',
  'June',
  'July',
  'August',
  'September',
  'October',
  'November',
  'December',
]

function isSameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  )
}

function isInRange(date: Date, start: Date | null, end: Date | null): boolean {
  if (!start || !end) return false
  const time = date.getTime()
  return time >= start.getTime() && time <= end.getTime()
}

function getCalendarDays(
  year: number,
  month: number,
  selected: { start: Date | null; end: Date | null },
  minDate?: Date,
  maxDate?: Date
): CalendarDay[] {
  const today = new Date()
  const firstDay = new Date(year, month, 1)
  const lastDay = new Date(year, month + 1, 0)
  const startPadding = firstDay.getDay()
  const days: CalendarDay[] = []

  // Add days from previous month
  const prevMonthLastDay = new Date(year, month, 0)
  for (let i = startPadding - 1; i >= 0; i--) {
    const date = new Date(year, month - 1, prevMonthLastDay.getDate() - i)
    days.push(createDay(date, false, today, selected, minDate, maxDate))
  }

  // Add days from current month
  for (let d = 1; d <= lastDay.getDate(); d++) {
    const date = new Date(year, month, d)
    days.push(createDay(date, true, today, selected, minDate, maxDate))
  }

  // Add days from next month to fill the grid
  const remaining = 42 - days.length // 6 rows of 7 days
  for (let d = 1; d <= remaining; d++) {
    const date = new Date(year, month + 1, d)
    days.push(createDay(date, false, today, selected, minDate, maxDate))
  }

  return days
}

function createDay(
  date: Date,
  isCurrentMonth: boolean,
  today: Date,
  selected: { start: Date | null; end: Date | null },
  minDate?: Date,
  maxDate?: Date
): CalendarDay {
  const isStart = selected.start ? isSameDay(date, selected.start) : false
  const isEnd = selected.end ? isSameDay(date, selected.end) : false
  const inRange = isInRange(date, selected.start, selected.end)

  let isDisabled = false
  if (minDate && date < minDate) isDisabled = true
  if (maxDate && date > maxDate) isDisabled = true

  return {
    date,
    isCurrentMonth,
    isToday: isSameDay(date, today),
    isSelected: isStart || isEnd,
    isInRange: inRange && !isStart && !isEnd,
    isRangeStart: isStart,
    isRangeEnd: isEnd,
    isDisabled,
  }
}

function Calendar({
  selected = { start: null, end: null },
  onSelect,
  mode = 'range',
  minDate,
  maxDate,
  className,
}: CalendarProps) {
  const [viewDate, setViewDate] = React.useState(() => {
    if (selected.start) return new Date(selected.start)
    return new Date()
  })

  const [rangeSelection, setRangeSelection] = React.useState<{
    start: Date | null
    end: Date | null
  }>(selected)

  React.useEffect(() => {
    setRangeSelection(selected)
  }, [selected])

  const year = viewDate.getFullYear()
  const month = viewDate.getMonth()
  const days = getCalendarDays(year, month, rangeSelection, minDate, maxDate)

  const goToPreviousMonth = () => {
    setViewDate(new Date(year, month - 1, 1))
  }

  const goToNextMonth = () => {
    setViewDate(new Date(year, month + 1, 1))
  }

  const handleDayClick = (day: CalendarDay) => {
    if (day.isDisabled) return

    if (mode === 'single') {
      const newSelection = { start: day.date, end: day.date }
      setRangeSelection(newSelection)
      onSelect?.(newSelection)
      return
    }

    // Range mode
    if (!rangeSelection.start || (rangeSelection.start && rangeSelection.end)) {
      // Start new range
      const newSelection = { start: day.date, end: null }
      setRangeSelection(newSelection)
      onSelect?.(newSelection as { start: Date; end: Date | null })
    } else {
      // Complete range
      let start = rangeSelection.start
      let end = day.date

      // Ensure start is before end
      if (end < start) {
        ;[start, end] = [end, start]
      }

      const newSelection = { start, end }
      setRangeSelection(newSelection)
      onSelect?.(newSelection)
    }
  }

  return (
    <div data-slot="calendar" className={cn('w-[280px] p-3', className)}>
      {/* Header */}
      <div className="mb-2 flex items-center justify-between">
        <button
          type="button"
          onClick={goToPreviousMonth}
          className="hover:bg-accent inline-flex h-7 w-7 items-center justify-center rounded-md transition-colors"
          aria-label="Previous month"
        >
          <ChevronLeft className="h-4 w-4" />
        </button>
        <span className="text-sm font-medium">
          {MONTHS[month]} {year}
        </span>
        <button
          type="button"
          onClick={goToNextMonth}
          className="hover:bg-accent inline-flex h-7 w-7 items-center justify-center rounded-md transition-colors"
          aria-label="Next month"
        >
          <ChevronRight className="h-4 w-4" />
        </button>
      </div>

      {/* Weekday headers */}
      <div className="mb-1 grid grid-cols-7 gap-1">
        {WEEKDAYS.map((day) => (
          <div
            key={day}
            className="text-muted-foreground flex h-8 w-8 items-center justify-center text-center text-xs font-medium"
          >
            {day}
          </div>
        ))}
      </div>

      {/* Days grid */}
      <div className="grid grid-cols-7 gap-1">
        {days.map((day, i) => (
          <button
            key={i}
            type="button"
            disabled={day.isDisabled}
            onClick={() => handleDayClick(day)}
            className={cn(
              'inline-flex h-8 w-8 items-center justify-center rounded-md text-sm transition-colors',
              // Base states
              !day.isCurrentMonth && 'text-muted-foreground/50',
              day.isDisabled && 'cursor-not-allowed opacity-30',
              !day.isDisabled && !day.isSelected && 'hover:bg-accent',
              // Today
              day.isToday && !day.isSelected && 'border-primary border',
              // Selected states
              day.isSelected && 'bg-primary text-primary-foreground',
              day.isRangeStart && 'rounded-r-none',
              day.isRangeEnd && 'rounded-l-none',
              // In range
              day.isInRange && 'bg-primary/20 rounded-none'
            )}
          >
            {day.date.getDate()}
          </button>
        ))}
      </div>
    </div>
  )
}
Calendar.displayName = 'Calendar'

export { Calendar }
