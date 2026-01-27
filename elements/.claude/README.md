# Claude Code Setup for Elements

Frontend-focused Claude Code configuration for the `@gram-ai/elements` component library.

## Directory Structure

```
.claude/
├── settings.json         # Permissions for frontend dev
├── commands/
│   ├── component.md      # /component - Create new components
│   ├── story.md          # /story - Create Storybook stories
│   ├── hook.md           # /hook - Create custom hooks
│   ├── a11y.md           # /a11y - Accessibility audit
│   └── cleanup.md        # /cleanup - Fix code issues
└── agents/
    ├── component-architect.md  # Component design patterns
    ├── style-reviewer.md       # CSS/Tailwind review
    └── test-writer.md          # Test writing guidance
```

## Slash Commands

| Command | Description | Example |
|---------|-------------|---------|
| `/component` | Create a new component | `/component Card ui` |
| `/story` | Create a Storybook story | `/story Button` |
| `/hook` | Create a custom hook | `/hook useDebounce` |
| `/a11y` | Audit accessibility | `/a11y src/components/ui/dialog.tsx` |
| `/cleanup` | Fix code issues | `/cleanup console` |

## Agents

### component-architect
Design component APIs using composition, compound components, and render props patterns.

```
Use the component-architect agent to design a new Tooltip component
```

### style-reviewer
Review Tailwind classes, CVA variants, and theme token usage.

```
Use the style-reviewer agent to review the Button component styling
```

### test-writer
Write comprehensive tests using Vitest and React Testing Library.

```
Use the test-writer agent to create tests for useToggle hook
```

## Quick Commands

```bash
# Development
pnpm dev              # Start Storybook
pnpm build            # Build library
pnpm test             # Run tests
pnpm lint             # Lint code
pnpm format           # Format code

# Type checking
pnpm tsc --noEmit
```

## Patterns to Follow

### Imports
Always use `@/` alias:
```tsx
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
```

### Components
- Use `forwardRef` for DOM access
- Set `displayName` for DevTools
- Use CVA for variants
- Use `cn()` for class merging

### Styling
- Never hardcode colors (use theme tokens)
- Use Tailwind utilities
- Follow class order: layout → sizing → typography → visual → interactive

### Testing
- Test behavior, not implementation
- Use `userEvent` over `fireEvent`
- Query by role first (`getByRole`)

## Inherited from Root

This directory inherits commands from `/gram/.claude/commands/`:
- `/pr` - Create pull requests
- `/review` - Code review
- `/debug` - Debug issues

## Resources

- [Elements CLAUDE.md](../CLAUDE.md) - Component patterns
- [Storybook](http://localhost:6006) - Component docs
- [Radix UI](https://www.radix-ui.com/primitives) - Primitive components
- [CVA](https://cva.style/docs) - Class variance authority
