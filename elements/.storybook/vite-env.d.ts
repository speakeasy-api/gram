// Injected via Vite' `define` config
declare const __GRAM_API_URL__: string | undefined
declare const __GRAM_GIT_SHA__: string | undefined

interface ImportMetaEnv {
  readonly VITE_GRAM_ELEMENTS_STORYBOOK_PROJECT_SLUG?: string | undefined
  readonly VITE_GRAM_ELEMENTS_STORYBOOK_MCP_URL?: string | undefined
  readonly STORYBOOK_CHROMATIC?: string | undefined
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
