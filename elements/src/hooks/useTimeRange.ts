import { useState, useCallback, useMemo, useRef, useEffect } from 'react'
import type { TimeRange, TimeRangePreset } from '@/lib/time-parser'
import {
  parseTimeRange,
  parseWithLLM,
  describeTimeRange,
  applyPreset,
  DEFAULT_PRESETS,
} from '@/lib/time-parser'

export interface UseTimeRangeOptions {
  /** Initial time range value */
  initialValue?: TimeRange
  /** Initial input text */
  initialInput?: string
  /** Enable LLM parsing for complex inputs */
  enableLLMParsing?: boolean
  /** API URL for LLM parsing (required if enableLLMParsing is true) */
  apiUrl?: string
  /** Auth headers for LLM parsing */
  authHeaders?: Record<string, string>
  /** Timezone for LLM parsing context */
  timezone?: string
  /** Debounce delay for parsing (ms) */
  debounceMs?: number
  /** Callback when time range changes */
  onChange?: (range: TimeRange) => void
}

export interface UseTimeRangeReturn {
  // State
  inputValue: string
  parsedRange: TimeRange | null
  interpretation: string
  isLive: boolean
  isParsing: boolean
  parseError: string | null

  // Actions
  setInputValue: (value: string) => void
  setLive: (isLive: boolean) => void
  selectPreset: (preset: TimeRangePreset) => void
  selectDateRange: (range: { start: Date; end: Date | null }) => void
  clear: () => void

  // Utilities
  presets: TimeRangePreset[]
}

export function useTimeRange(
  options: UseTimeRangeOptions = {}
): UseTimeRangeReturn {
  const {
    initialValue,
    initialInput = '',
    enableLLMParsing = false,
    apiUrl,
    authHeaders = {},
    timezone,
    debounceMs = 500,
    onChange,
  } = options

  // State
  const [inputValue, setInputValueInternal] = useState(initialInput)
  const [parsedRange, setParsedRange] = useState<TimeRange | null>(
    initialValue ?? null
  )
  const [isLive, setIsLive] = useState(false)
  const [isParsing, setIsParsing] = useState(false)
  const [parseError, setParseError] = useState<string | null>(null)

  // Refs for debouncing
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const onChangeRef = useRef(onChange)
  onChangeRef.current = onChange

  // Live mode interval
  const liveIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Update end time when live mode is active
  useEffect(() => {
    if (isLive && parsedRange) {
      // Refresh end time immediately
      const refreshEndTime = () => {
        setParsedRange((prev) => {
          if (!prev) return prev
          const now = new Date()
          const newRange = { ...prev, end: now }
          onChangeRef.current?.(newRange)
          return newRange
        })
      }

      // Refresh every second
      liveIntervalRef.current = setInterval(refreshEndTime, 1000)
      return () => {
        if (liveIntervalRef.current) {
          clearInterval(liveIntervalRef.current)
        }
      }
    }
  }, [isLive, parsedRange])

  // Parse input with debouncing
  const parseInput = useCallback(
    async (input: string) => {
      const trimmed = input.trim()
      if (!trimmed) {
        setParsedRange(null)
        setParseError(null)
        return
      }

      // Try native parsing first (synchronous)
      const now = new Date()
      const nativeResult = parseTimeRange(trimmed, now)

      if (nativeResult) {
        setParsedRange(nativeResult)
        setParseError(null)
        onChangeRef.current?.(nativeResult)
        return
      }

      // If LLM parsing is enabled and native parsing failed, try LLM
      if (enableLLMParsing && apiUrl) {
        setIsParsing(true)
        try {
          const llmResult = await parseWithLLM(
            trimmed,
            apiUrl,
            authHeaders,
            timezone,
            now
          )
          if (llmResult) {
            setParsedRange(llmResult)
            setParseError(null)
            onChangeRef.current?.(llmResult)
          } else {
            setParseError('Could not parse time range')
          }
        } catch {
          setParseError('Failed to parse time range')
        } finally {
          setIsParsing(false)
        }
      } else {
        // No LLM, native parsing failed
        setParseError('Could not parse time range')
      }
    },
    [enableLLMParsing, apiUrl, authHeaders, timezone]
  )

  // Debounced input handler
  const setInputValue = useCallback(
    (value: string) => {
      setInputValueInternal(value)
      setParseError(null)

      // Clear existing debounce
      if (debounceRef.current) {
        clearTimeout(debounceRef.current)
      }

      // Debounce parsing
      debounceRef.current = setTimeout(() => {
        parseInput(value)
      }, debounceMs)
    },
    [parseInput, debounceMs]
  )

  // Select a preset
  const selectPreset = useCallback((preset: TimeRangePreset) => {
    const now = new Date()
    const range = applyPreset(preset, now)
    setInputValueInternal(preset.label)
    setParsedRange(range)
    setParseError(null)
    onChangeRef.current?.(range)
  }, [])

  // Select from calendar
  const selectDateRange = useCallback(
    (range: { start: Date; end: Date | null }) => {
      if (range.start && range.end) {
        const fullRange = { start: range.start, end: range.end }
        setParsedRange(fullRange)
        setInputValueInternal('')
        setParseError(null)
        onChangeRef.current?.(fullRange)
      }
    },
    []
  )

  // Toggle live mode
  const setLive = useCallback((live: boolean) => {
    setIsLive(live)
    if (live) {
      // Update end time to now when enabling live mode
      setParsedRange((prev) => {
        if (!prev) return prev
        return { ...prev, end: new Date() }
      })
    }
  }, [])

  // Clear everything
  const clear = useCallback(() => {
    setInputValueInternal('')
    setParsedRange(null)
    setParseError(null)
    setIsLive(false)
    if (debounceRef.current) {
      clearTimeout(debounceRef.current)
    }
  }, [])

  // Compute interpretation string
  const interpretation = useMemo(() => {
    if (!parsedRange) return ''
    return describeTimeRange(parsedRange)
  }, [parsedRange])

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current)
      }
      if (liveIntervalRef.current) {
        clearInterval(liveIntervalRef.current)
      }
    }
  }, [])

  return {
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
    presets: DEFAULT_PRESETS,
  }
}
