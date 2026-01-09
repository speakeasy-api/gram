interface ImportMetaEnv {
  readonly VITE_GRAM_ELEMENTS_STORYBOOK_PROJECT_SLUG?: string | undefined
  readonly VITE_GRAM_ELEMENTS_STORYBOOK_MCP_URL?: string | undefined
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
