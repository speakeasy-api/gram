// For more info, see https://github.com/storybookjs/eslint-plugin-storybook#configuration-flat-config-format
import storybook from 'eslint-plugin-storybook'

// @ts-check

import eslint from '@eslint/js'
import tseslint from 'typescript-eslint'
import { includeIgnoreFile } from '@eslint/compat'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import prettier from 'eslint-plugin-prettier'
import reactRefresh from 'eslint-plugin-react-refresh'
import unusedImports from 'eslint-plugin-unused-imports'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)
const gitignorePath = path.resolve(__dirname, '.gitignore')

export default tseslint.config(
  eslint.configs.recommended,
  ...tseslint.configs.recommended,
  includeIgnoreFile(gitignorePath),
  ...storybook.configs['flat/recommended'],
  {
    ignores: ['scripts/generate-utility-docs.js', 'bin/cli.js', '.storybook/**'],
  },
  {
    languageOptions: {
      globals: {
        console: true,
      },
      parserOptions: {
        tsconfigRootDir: __dirname,
        project: true,
      },
    },
  },
  {
    plugins: {
      prettier,
      'react-refresh': reactRefresh,
      'unused-imports': unusedImports,
    },
    rules: {
      'prettier/prettier': ['error', {}, { usePrettierrc: true }],
      'no-unused-vars': 'off', // or "@typescript-eslint/no-unused-vars": "off",
      'unused-imports/no-unused-imports': 'error',

      'storybook/no-redundant-story-name': 'off',

      'react-refresh/only-export-components': 'error',

      'unused-imports/no-unused-vars': [
        'warn',
        {
          vars: 'all',
          varsIgnorePattern: '^_',
          args: 'after-used',
          argsIgnorePattern: '^_',
        },
      ],
    },
  },
  // Context files commonly export both providers and hooks
  {
    files: ['src/contexts/**/*.tsx'],
    rules: {
      'react-refresh/only-export-components': 'off',
    },
  }
)
