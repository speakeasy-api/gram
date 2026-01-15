#!/usr/bin/env node

/**
 * Migration script to add 'gramel:' prefix to all Tailwind CSS classes
 * in the elements library.
 *
 * Usage: node scripts/migrate-prefix.mjs [--dry-run]
 */

import { readFileSync, writeFileSync, readdirSync, statSync } from 'fs'
import { join, extname } from 'path'

const DRY_RUN = process.argv.includes('--dry-run')
const SRC_DIR = new URL('../src', import.meta.url).pathname

// Classes that should NOT be prefixed (custom classes, not Tailwind utilities)
const SKIP_PREFIXES = [
  'aui-', // assistant-ui custom classes
]

// Known Tailwind class patterns (starting tokens)
// This list covers the vast majority of Tailwind utilities
const TAILWIND_PATTERNS = [
  // Layout
  /^(block|inline-block|inline|flex|inline-flex|table|inline-table|table-caption|table-cell|table-column|table-column-group|table-footer-group|table-header-group|table-row-group|table-row|flow-root|grid|inline-grid|contents|list-item|hidden)$/,
  /^(static|fixed|absolute|relative|sticky)$/,
  /^(visible|invisible|collapse)$/,
  /^(isolate|isolation-auto)$/,
  /^(z-|inset-|top-|right-|bottom-|left-)/,
  /^(float-|clear-|object-|overflow-|overscroll-)/,

  // Flexbox & Grid
  /^(basis-|flex-|grow|shrink|order-)/,
  /^(grid-|col-|row-|auto-rows-|auto-cols-)/,
  /^(gap-|justify-|content-|items-|self-|place-)/,

  // Spacing
  /^(p-|px-|py-|pt-|pr-|pb-|pl-|ps-|pe-)/,
  /^(m-|mx-|my-|mt-|mr-|mb-|ml-|ms-|me-|-m)/,
  /^(space-)/,

  // Sizing
  /^(w-|min-w-|max-w-|h-|min-h-|max-h-|size-)/,

  // Typography
  /^(font-|text-|tracking-|leading-|list-|placeholder-)/,
  /^(decoration-|underline|overline|line-through|no-underline)/,
  /^(uppercase|lowercase|capitalize|normal-case)/,
  /^(truncate|indent-|align-|whitespace-|break-|hyphens-)/,
  /^(antialiased|subpixel-antialiased)/,
  /^(italic|not-italic)/,
  /^(ordinal|slashed-zero|lining-nums|oldstyle-nums|proportional-nums|tabular-nums|diagonal-fractions|stacked-fractions|normal-nums)/,
  /^wrap-/,

  // Backgrounds
  /^(bg-|from-|via-|to-|gradient-)/,

  // Borders
  /^(border|rounded|divide-|outline-|ring-)/,

  // Effects
  /^(shadow-|opacity-|mix-blend-|bg-blend-)/,

  // Filters
  /^(blur-|brightness-|contrast-|drop-shadow-|grayscale|hue-rotate-|invert|saturate-|sepia|backdrop-)/,
  /^(filter|backdrop-filter)/,

  // Tables
  /^(table-|border-collapse|border-separate|border-spacing-|caption-)/,

  // Transitions & Animation
  /^(transition|duration-|ease-|delay-|animate-)/,

  // Transforms
  /^(scale-|rotate-|translate-|skew-|origin-|transform)/,
  /^(-translate-|-rotate-|-skew-)/,

  // Interactivity
  /^(accent-|appearance-|cursor-|caret-|pointer-events-|resize|scroll-|snap-|touch-|select-|will-change-)/,

  // SVG
  /^(fill-|stroke-)/,

  // Accessibility
  /^(sr-only|not-sr-only)/,

  // Container
  /^(container)$/,
  /^(@container|@md|@lg|@xl|@2xl|@3xl)/,

  // Aspect ratio
  /^(aspect-)/,

  // Columns
  /^(columns-|break-)/,

  // Box
  /^(box-)/,

  // Display utilities
  /^(group|peer)/,

  // States/variants at the start (these are part of the class, not separate)
  /^(hover:|focus:|active:|disabled:|visited:|first:|last:|odd:|even:|focus-within:|focus-visible:|motion-safe:|motion-reduce:|dark:|print:|portrait:|landscape:|ltr:|rtl:|open:|placeholder-shown:|autofill:|read-only:|checked:|indeterminate:|default:|required:|valid:|invalid:|in-range:|out-of-range:|enabled:|not-|group-|peer-|has-|aria-|data-|nth-|\*:)/,

  // Arbitrary values
  /^\[.+\]$/,

  // Negative values
  /^-/,

  // Color utilities
  /^(slate-|gray-|zinc-|neutral-|stone-|red-|orange-|amber-|yellow-|lime-|green-|emerald-|teal-|cyan-|sky-|blue-|indigo-|violet-|purple-|fuchsia-|pink-|rose-)/,
  /^(current|transparent|black|white)/,

  // Line clamp
  /^(line-clamp-)/,

  // Forced colors
  /^(forced-color-adjust-)/,

  // Accent colors (custom theme)
  /^(primary|secondary|destructive|muted|accent|popover|card|background|foreground|border|input|ring|chart-|sidebar)/,
]

// Check if a class should be prefixed
function shouldPrefix(className) {
  // Skip if it already has the prefix
  if (className.startsWith('gramel:')) {
    return false
  }

  // Skip custom classes
  for (const prefix of SKIP_PREFIXES) {
    if (className.startsWith(prefix)) {
      return false
    }
  }

  // Skip empty or whitespace
  if (!className.trim()) {
    return false
  }

  // Strip variant prefixes to check the base utility
  let baseClass = className

  // Handle arbitrary variants like [&_svg]:utility or [&>*]:utility
  // Also handles nested brackets like [&_svg:not([class*='size-'])]:size-4
  if (baseClass.startsWith('[')) {
    // Count brackets to find the matching close bracket
    let depth = 0
    let colonAfterBracket = -1
    for (let i = 0; i < baseClass.length; i++) {
      if (baseClass[i] === '[') depth++
      else if (baseClass[i] === ']') {
        depth--
        if (depth === 0 && i + 1 < baseClass.length && baseClass[i + 1] === ':') {
          colonAfterBracket = i + 1
          break
        }
      }
    }
    if (colonAfterBracket !== -1) {
      baseClass = baseClass.substring(colonAfterBracket + 1)
    }
  }

  // Handle has-[selector]:utility, group-has-[selector]:utility, etc.
  const arbitraryVariantMatch = baseClass.match(/^(has|not|group-has|peer-has|group|peer)-\[[^\]]+\]:(.+)$/)
  if (arbitraryVariantMatch) {
    baseClass = arbitraryVariantMatch[2]
  }

  const variantPrefixes = ['hover:', 'focus:', 'active:', 'disabled:', 'group-hover:', 'group-focus:', 'peer-hover:', 'peer-focus:', 'dark:', 'sm:', 'md:', 'lg:', 'xl:', '2xl:', 'first:', 'last:', 'odd:', 'even:', 'focus-within:', 'focus-visible:', 'aria-invalid:', 'aria-', 'data-', 'has-', 'nth-', '*:', 'not-', 'group/', 'group-', 'peer-', 'open:', 'checked:', 'required:', 'valid:', 'invalid:', 'placeholder-shown:', 'autofill:', 'read-only:', 'indeterminate:', 'default:', 'in-range:', 'out-of-range:', 'enabled:', 'motion-safe:', 'motion-reduce:', 'print:', 'portrait:', 'landscape:', 'ltr:', 'rtl:', 'visited:', '@md:', '@lg:', '@xl:', '@container', 'data-floating:', 'data-state=', 'data-[']

  // Keep stripping variant prefixes
  let changed = true
  while (changed) {
    changed = false
    for (const vp of variantPrefixes) {
      if (baseClass.startsWith(vp)) {
        // Find where the variant prefix ends
        if (vp.includes('[')) {
          // Handle data-[...]: pattern
          const bracketEnd = baseClass.indexOf(']:')
          if (bracketEnd !== -1) {
            baseClass = baseClass.substring(bracketEnd + 2)
            changed = true
          }
        } else {
          baseClass = baseClass.substring(vp.length)
          changed = true
        }
      }
    }
  }

  // Check if the base class matches Tailwind patterns
  for (const pattern of TAILWIND_PATTERNS) {
    if (pattern.test(baseClass)) {
      return true
    }
  }

  // Additional check for common utilities that might be missed
  const commonUtilities = [
    'flex', 'grid', 'block', 'inline', 'hidden', 'relative', 'absolute', 'fixed', 'sticky',
    'grow', 'shrink', 'auto', 'none', 'full', 'screen', 'min', 'max', 'fit',
    'start', 'end', 'center', 'between', 'around', 'evenly', 'stretch', 'baseline',
    'wrap', 'nowrap', 'reverse', 'row', 'col', 'column',
    'visible', 'invisible', 'underline', 'overline', 'italic', 'antialiased',
    'truncate', 'uppercase', 'lowercase', 'capitalize', 'normal-case',
    'border', 'rounded', 'shadow', 'ring', 'outline',
    'transition', 'transform', 'filter', 'container',
    'sr-only', 'not-sr-only', 'resize', 'snap', 'scroll', 'touch', 'select',
    'cursor', 'pointer', 'appearance', 'will-change',
  ]

  if (commonUtilities.includes(baseClass)) {
    return true
  }

  // Check for size patterns like size-4, w-full, etc.
  if (/^[a-z]+-[a-z0-9/.[\]]+$/.test(baseClass)) {
    return true
  }

  // Check for negative values
  if (/^-[a-z]+-/.test(baseClass)) {
    return true
  }

  return false
}

// Add prefix to a single class
function prefixClass(className) {
  // Don't prefix if it's inside a CSS selector (part of attribute selector like [class*='size-'])
  // These are detected as tokens starting with quotes
  if (className.startsWith("'") || className.startsWith('"')) {
    return className
  }

  if (shouldPrefix(className)) {
    return `gramel:${className}`
  }
  return className
}

// Process a class string (space-separated classes)
function processClassString(classString) {
  // Split by whitespace, preserving empty strings for multiple spaces
  const classes = classString.split(/(\s+)/)
  return classes.map(part => {
    // If it's whitespace, keep it as is
    if (/^\s+$/.test(part)) {
      return part
    }
    // Otherwise, it's a class name
    return prefixClass(part)
  }).join('')
}

// Check if a string looks like it contains Tailwind classes
function looksLikeClassString(str) {
  // Empty or very short strings are unlikely to be class strings
  if (!str || str.length < 2) return false

  // If it contains Tailwind-like patterns, it's probably a class string
  const tailwindPatterns = [
    /\bflex\b/, /\bgrid\b/, /\bhidden\b/, /\bblock\b/, /\brelative\b/, /\babsolute\b/,
    /\bp-\d/, /\bm-\d/, /\bpx-/, /\bpy-/, /\bmx-/, /\bmy-/, /\bpt-/, /\bpb-/, /\bpl-/, /\bpr-/,
    /\bw-/, /\bh-/, /\bsize-/, /\bmin-/, /\bmax-/,
    /\bbg-/, /\btext-/, /\bborder/, /\brounded/, /\bshadow/, /\bring-/, /\boutline-/,
    /\bfont-/, /\bleading-/, /\btracking-/,
    /\bitems-/, /\bjustify-/, /\bgap-/, /\bspace-/,
    /\bhover:/, /\bfocus:/, /\bdark:/, /\bdisabled:/, /\bactive:/,
    /\btransition/, /\bduration-/, /\bease-/, /\bdelay-/, /\banimate-/,
    /\boverflow-/, /\bz-/, /\btop-/, /\bright-/, /\bbottom-/, /\bleft-/,
    /\bopacity-/, /\bcursor-/, /\bpointer-events-/,
    /\[&_/, // Arbitrary selectors like [&_svg]:
  ]

  return tailwindPatterns.some(pattern => pattern.test(str))
}

// Process file content
function processFile(content, filePath) {
  let modified = content
  let changeCount = 0

  // Process className="..." (double quotes)
  modified = modified.replace(
    /className="([^"]*)"/g,
    (match, classes) => {
      const newClasses = processClassString(classes)
      if (newClasses !== classes) {
        changeCount++
      }
      return `className="${newClasses}"`
    }
  )

  // Process className='...' (single quotes)
  modified = modified.replace(
    /className='([^']*)'/g,
    (match, classes) => {
      const newClasses = processClassString(classes)
      if (newClasses !== classes) {
        changeCount++
      }
      return `className='${newClasses}'`
    }
  )

  // Process className={`...`} (template literals without expressions)
  modified = modified.replace(
    /className=\{`([^`]*)`\}/g,
    (match, classes) => {
      const newClasses = processClassString(classes)
      if (newClasses !== classes) {
        changeCount++
      }
      return `className={\`${newClasses}\`}`
    }
  )

  // Process cn('...'), cn("...") calls - handle ALL string literals inside cn()
  // This regex matches cn( followed by any string argument
  modified = modified.replace(
    /\bcn\(\s*(['"])([^'"]*)\1/g,
    (match, quote, classes) => {
      const newClasses = processClassString(classes)
      if (newClasses !== classes) {
        changeCount++
      }
      return `cn(${quote}${newClasses}${quote}`
    }
  )

  // Process cva("...") - the base classes (double quotes, may contain single quotes)
  modified = modified.replace(
    /\bcva\(\s*"([^"]*)"/g,
    (match, classes) => {
      const newClasses = processClassString(classes)
      if (newClasses !== classes) {
        changeCount++
      }
      return `cva("${newClasses}"`
    }
  )

  // Process cva('...') - the base classes (single quotes, may contain double quotes)
  modified = modified.replace(
    /\bcva\(\s*'([^']*)'/g,
    (match, classes) => {
      const newClasses = processClassString(classes)
      if (newClasses !== classes) {
        changeCount++
      }
      return `cva('${newClasses}'`
    }
  )

  // Process ALL double-quoted string literals that look like Tailwind class strings
  modified = modified.replace(
    /"([^"]{3,})"/g,
    (match, str) => {
      // Skip if already processed (has gramel: prefix)
      if (str.includes('gramel:')) return match

      // Skip if it doesn't look like a class string
      if (!looksLikeClassString(str)) return match

      // Skip import/export statements and URLs
      if (str.includes('/') && (str.startsWith('@') || str.startsWith('http') || str.startsWith('.'))) return match

      // Skip strings that look like CSS selectors or attribute selectors
      if (str.includes('[class*=') || str.includes('[class^=') || str.includes('[class$=') || str.includes('[class~=')) return match

      const newStr = processClassString(str)
      if (newStr !== str) {
        changeCount++
        return `"${newStr}"`
      }
      return match
    }
  )

  // Process ALL single-quoted string literals that look like Tailwind class strings
  // Use negative lookbehind to skip attribute selectors like [class*='...']
  modified = modified.replace(
    /(?<!\[class\*=)'([^']{3,})'/g,
    (match, str) => {
      // Skip if already processed (has gramel: prefix)
      if (str.includes('gramel:')) return match

      // Skip if it doesn't look like a class string
      if (!looksLikeClassString(str)) return match

      // Skip import/export statements and URLs
      if (str.includes('/') && (str.startsWith('@') || str.startsWith('http') || str.startsWith('.'))) return match

      // Skip strings that look like CSS selectors
      if (str.includes('[class*=')) return match

      const newStr = processClassString(str)
      if (newStr !== str) {
        changeCount++
        return `'${newStr}'`
      }
      return match
    }
  )

  return { content: modified, changeCount }
}

// Get all TypeScript/TSX files recursively
function getFiles(dir, extensions = ['.ts', '.tsx']) {
  const files = []
  const entries = readdirSync(dir)

  for (const entry of entries) {
    const fullPath = join(dir, entry)
    const stat = statSync(fullPath)

    if (stat.isDirectory()) {
      // Skip node_modules and other common directories
      if (!['node_modules', 'dist', '.git', '.storybook'].includes(entry)) {
        files.push(...getFiles(fullPath, extensions))
      }
    } else if (extensions.includes(extname(entry))) {
      files.push(fullPath)
    }
  }

  return files
}

// Main function
function main() {
  console.log(`🔄 Starting prefix migration${DRY_RUN ? ' (dry run)' : ''}...`)
  console.log(`📁 Source directory: ${SRC_DIR}\n`)

  const files = getFiles(SRC_DIR)
  let totalChanges = 0
  let filesModified = 0

  for (const file of files) {
    const content = readFileSync(file, 'utf-8')
    const { content: modified, changeCount } = processFile(content, file)

    if (modified !== content) {
      filesModified++
      totalChanges += changeCount
      const relativePath = file.replace(SRC_DIR, 'src')
      console.log(`✏️  ${relativePath} (${changeCount} changes)`)

      if (!DRY_RUN) {
        writeFileSync(file, modified, 'utf-8')
      }
    }
  }

  console.log(`\n✅ Migration complete!`)
  console.log(`   Files modified: ${filesModified}`)
  console.log(`   Total changes: ${totalChanges}`)

  if (DRY_RUN) {
    console.log(`\n⚠️  This was a dry run. No files were actually modified.`)
    console.log(`   Run without --dry-run to apply changes.`)
  }
}

main()
