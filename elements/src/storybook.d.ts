/**
 * Storybook type augmentations for Gram Elements.
 *
 * This file extends Storybook's Parameters interface to provide
 * type safety for the `elements` parameter used in stories.
 */
import type { ElementsConfig } from './types'

/**
 * Configuration for the elements decorator in Storybook stories.
 */
export interface ElementsParameters {
  /**
   * Partial configuration that will be merged with the default Elements config.
   * Use this to override specific config values per-story.
   */
  config?: Partial<ElementsConfig>
}

declare module 'storybook/internal/csf' {
  interface Parameters {
    /**
     * Custom parameters for the Gram Elements decorator.
     * The config is passed to ElementsProvider and merged with defaults.
     */
    elements?: ElementsParameters
  }
}
