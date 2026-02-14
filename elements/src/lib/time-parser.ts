/**
 * Time range parsing utilities for the TimeRangePicker component.
 * Supports natural language input like "3 days ago", "last week", "15m", etc.
 */

export interface TimeRange {
  start: Date
  end: Date
}

export interface TimeRangePreset {
  label: string
  value: string
  duration: number // milliseconds
}

export const DEFAULT_PRESETS: TimeRangePreset[] = [
  { label: '15m', value: '15m', duration: 15 * 60 * 1000 },
  { label: '1h', value: '1h', duration: 60 * 60 * 1000 },
  { label: '24h', value: '24h', duration: 24 * 60 * 60 * 1000 },
  { label: '7d', value: '7d', duration: 7 * 24 * 60 * 60 * 1000 },
  { label: '30d', value: '30d', duration: 30 * 24 * 60 * 60 * 1000 },
]

const DURATION_REGEX =
  /^(\d+)\s*(m|min|mins|minutes?|h|hr|hrs|hours?|d|days?)$/i
const RELATIVE_AGO_REGEX =
  /^(\d+)\s*(minutes?|hours?|days?|weeks?|months?)\s+ago$/i
const ISO_DATE_REGEX = /^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2}:\d{2})?/

/**
 * Parse a natural language time input into a TimeRange.
 * Returns null if the input cannot be parsed.
 */
export function parseTimeRange(
  input: string,
  now: Date = new Date()
): TimeRange | null {
  const trimmed = input.trim().toLowerCase()

  if (!trimmed) {
    return null
  }

  // Try duration shortcuts first (e.g., "15m", "1h", "24h", "7d")
  const durationMatch = trimmed.match(DURATION_REGEX)
  if (durationMatch) {
    const value = parseInt(durationMatch[1], 10)
    const unit = durationMatch[2].charAt(0).toLowerCase()

    let ms = 0
    switch (unit) {
      case 'm':
        ms = value * 60 * 1000
        break
      case 'h':
        ms = value * 60 * 60 * 1000
        break
      case 'd':
        ms = value * 24 * 60 * 60 * 1000
        break
    }

    if (ms > 0) {
      return {
        start: new Date(now.getTime() - ms),
        end: now,
      }
    }
  }

  // Try relative expressions (e.g., "3 days ago", "2 hours ago")
  const relativeMatch = trimmed.match(RELATIVE_AGO_REGEX)
  if (relativeMatch) {
    const value = parseInt(relativeMatch[1], 10)
    const unit = relativeMatch[2].toLowerCase()

    let ms = 0
    if (unit.startsWith('minute')) {
      ms = value * 60 * 1000
    } else if (unit.startsWith('hour')) {
      ms = value * 60 * 60 * 1000
    } else if (unit.startsWith('day')) {
      ms = value * 24 * 60 * 60 * 1000
    } else if (unit.startsWith('week')) {
      ms = value * 7 * 24 * 60 * 60 * 1000
    } else if (unit.startsWith('month')) {
      ms = value * 30 * 24 * 60 * 60 * 1000
    }

    if (ms > 0) {
      return {
        start: new Date(now.getTime() - ms),
        end: now,
      }
    }
  }

  // Try relative words
  switch (trimmed) {
    case 'today': {
      const start = new Date(now)
      start.setHours(0, 0, 0, 0)
      return { start, end: now }
    }
    case 'yesterday': {
      const start = new Date(now)
      start.setDate(start.getDate() - 1)
      start.setHours(0, 0, 0, 0)
      const end = new Date(start)
      end.setHours(23, 59, 59, 999)
      return { start, end }
    }
    case 'this week': {
      const start = new Date(now)
      const day = start.getDay()
      const diff = start.getDate() - day + (day === 0 ? -6 : 1) // Adjust for Sunday
      start.setDate(diff)
      start.setHours(0, 0, 0, 0)
      return { start, end: now }
    }
    case 'last week': {
      const start = new Date(now)
      const day = start.getDay()
      const diff = start.getDate() - day + (day === 0 ? -6 : 1) - 7
      start.setDate(diff)
      start.setHours(0, 0, 0, 0)
      const end = new Date(start)
      end.setDate(end.getDate() + 6)
      end.setHours(23, 59, 59, 999)
      return { start, end }
    }
    case 'this month': {
      const start = new Date(now.getFullYear(), now.getMonth(), 1)
      return { start, end: now }
    }
    case 'last month': {
      const start = new Date(now.getFullYear(), now.getMonth() - 1, 1)
      const end = new Date(
        now.getFullYear(),
        now.getMonth(),
        0,
        23,
        59,
        59,
        999
      )
      return { start, end }
    }
  }

  // Try ISO date format
  if (ISO_DATE_REGEX.test(trimmed)) {
    const date = new Date(trimmed)
    if (!isNaN(date.getTime())) {
      return { start: date, end: now }
    }
  }

  return null
}

/**
 * Parse time range using LLM for complex natural language inputs.
 * Falls back to native parsing on error.
 */
export async function parseWithLLM(
  input: string,
  apiUrl: string,
  headers: Record<string, string>,
  timezone?: string,
  now: Date = new Date()
): Promise<TimeRange | null> {
  // First try native parsing
  const nativeResult = parseTimeRange(input, now)
  if (nativeResult) {
    return nativeResult
  }

  // Try LLM parsing for complex inputs
  try {
    const response = await fetch(`${apiUrl}/rpc/llm.parseTimeRange`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        ...headers,
      },
      body: JSON.stringify({
        input,
        timezone,
        referenceTime: now.toISOString(),
      }),
    })

    if (!response.ok) {
      console.warn('LLM time parsing failed:', response.statusText)
      return null
    }

    const data = await response.json()
    if (data.start && data.end) {
      return {
        start: new Date(data.start),
        end: new Date(data.end),
      }
    }
  } catch (error) {
    console.warn('LLM time parsing error:', error)
  }

  return null
}

/**
 * Format a TimeRange for display.
 */
export function formatTimeRange(
  range: TimeRange,
  options?: { includeTime?: boolean; timezone?: string }
): string {
  const { includeTime = true } = options ?? {}

  const dateOptions: Intl.DateTimeFormatOptions = {
    month: 'short',
    day: 'numeric',
    ...(includeTime && { hour: 'numeric', minute: '2-digit' }),
    ...(options?.timezone && { timeZone: options.timezone }),
  }

  const startStr = range.start.toLocaleString(undefined, dateOptions)
  const endStr = range.end.toLocaleString(undefined, dateOptions)

  return `${startStr} - ${endStr}`
}

/**
 * Get a human-readable description of a time range relative to now.
 */
export function describeTimeRange(
  range: TimeRange,
  now: Date = new Date()
): string {
  const diffMs = now.getTime() - range.start.getTime()
  const isEndNow = Math.abs(now.getTime() - range.end.getTime()) < 60000 // Within 1 minute

  if (!isEndNow) {
    return formatTimeRange(range)
  }

  const minutes = Math.round(diffMs / (60 * 1000))
  const hours = Math.round(diffMs / (60 * 60 * 1000))
  const days = Math.round(diffMs / (24 * 60 * 60 * 1000))

  if (minutes < 60) {
    return `Past ${minutes} minute${minutes !== 1 ? 's' : ''}`
  } else if (hours < 24) {
    return `Past ${hours} hour${hours !== 1 ? 's' : ''}`
  } else {
    return `Past ${days} day${days !== 1 ? 's' : ''}`
  }
}

/**
 * Apply a preset to get a time range.
 */
export function applyPreset(
  preset: TimeRangePreset,
  now: Date = new Date()
): TimeRange {
  return {
    start: new Date(now.getTime() - preset.duration),
    end: now,
  }
}
