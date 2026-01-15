import { datadogRum } from '@datadog/browser-rum'
import { DATADOG_CONFIG } from './errorTracking.config'

let initialized = false
let enabled = true

export interface ErrorTrackingConfig {
  enabled?: boolean
  projectSlug?: string
  variant?: string
}

export interface ErrorContext {
  source: 'error-boundary' | 'streaming' | 'stream-creation' | 'custom'
  componentStack?: string
  [key: string]: unknown
}

/**
 * Initialize Datadog RUM for error tracking.
 * Should be called once when the ElementsProvider mounts.
 */
export function initErrorTracking(config: ErrorTrackingConfig = {}): void {
  // Check if explicitly disabled
  if (config.enabled === false) {
    enabled = false
    return
  }

  // Prevent double initialization
  if (initialized) {
    return
  }

  // Skip if credentials not configured (e.g., local dev without env vars)
  if (!DATADOG_CONFIG.applicationId || !DATADOG_CONFIG.clientToken) {
    enabled = false
    return
  }

  try {
    datadogRum.init({
      applicationId: DATADOG_CONFIG.applicationId,
      clientToken: DATADOG_CONFIG.clientToken,
      site: DATADOG_CONFIG.site,
      service: DATADOG_CONFIG.service,
      env: process.env.NODE_ENV || 'production',
      sessionSampleRate: 100,
      sessionReplaySampleRate: 100,
      trackUserInteractions: true, // Focus on errors only
      trackResources: true,
      trackLongTasks: true,

      // Note: we need to mask everything, not just user input, as sensitive data may be echo-ed
      // back in the LLM messages or the user messages in the chat window
      defaultPrivacyLevel: 'mask',
    })

    // Set global context
    if (config.projectSlug) {
      datadogRum.setGlobalContextProperty('projectSlug', config.projectSlug)
    }
    if (config.variant) {
      datadogRum.setGlobalContextProperty('variant', config.variant)
    }

    initialized = true
  } catch (error) {
    console.warn('[Elements] Failed to initialize Datadog RUM:', error)
    enabled = false
  }
}

/**
 * Track an error to Datadog RUM.
 * Includes context about where the error originated.
 */
export function trackError(
  error: Error | unknown,
  context: ErrorContext
): void {
  if (!enabled || !initialized) {
    return
  }

  const errorObj = error instanceof Error ? error : new Error(String(error))

  try {
    datadogRum.addError(errorObj, {
      ...context,
      timestamp: new Date().toISOString(),
    })
  } catch (e) {
    // Silently fail - we don't want error tracking to cause more errors
    console.warn('[Elements] Failed to track error:', e)
  }
}

/**
 * Check if error tracking is currently enabled.
 */
export function isErrorTrackingEnabled(): boolean {
  return enabled && initialized
}
