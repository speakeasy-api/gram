# Moonshine Table renderRow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `renderRow` prop to moonshine's `Table` so consumers can wrap each data `<tr>` (e.g. in a Radix `ContextMenuTrigger asChild`).

**Architecture:** `RowContainer` (the internal component that owns the `<tr>`) becomes ref- and prop-forwarding so Radix `asChild`/`cloneElement` composition works. A new `renderRow?: (row, rowElement) => ReactNode` prop threads from `Table`/`Table.Body` through `Row`, `RowExpandable` (wraps the `<tr>` only, not the expanded panel), and `RowGroup`.

**Tech Stack:** React 18, TypeScript, vitest + Testing Library, Storybook 9, semantic-release (conventional commits).

## Global Constraints

- Repo: `/Users/sagar/moonshine`, worktree `/Users/sagar/moonshine/.claude/worktrees/table-render-row`, branch `feat/table-render-row`.
- Package manager pnpm; commands: `pnpm type-check`, `pnpm test`, `pnpm lint`, `pnpm build`.
- Commit messages must be Conventional Commits (`feat:` → minor release via semantic-release on merge to main).
- Existing story/test idioms: tests colocated as `index.test.tsx`, plain Testing Library `render`; stories in `index.stories.tsx` using `TableWithState`-style wrappers and seeded faker data.

---

### Task 1: renderRow prop + ref/prop-forwarding RowContainer

**Files:**
- Modify: `src/components/Table/index.tsx`
- Test: `src/components/Table/index.test.tsx` (new)

**Interfaces:**
- Produces: `TableProps<T>.renderRow?: (row: T, rowElement: React.ReactElement) => ReactNode`, same prop on `Table.Body` (`BodyProps<T>`). Exported type `RenderRow<T>`.

- [ ] **Step 1: Write the failing test** — create `src/components/Table/index.test.tsx`:

```tsx
import { render, screen, fireEvent } from '@testing-library/react'
import { cloneElement } from 'react'
import { expect, describe, it, vi } from 'vitest'
import { Column, Table } from '.'

type RowData = { id: number; name: string }
const columns: Column<RowData>[] = [{ key: 'name', header: 'Name' }]
const data: RowData[] = [
  { id: 1, name: 'alpha' },
  { id: 2, name: 'beta' },
]

describe('Table renderRow', () => {
  it('renders rows unchanged without renderRow', () => {
    render(<Table columns={columns} data={data} rowKey={(r) => r.id} />)
    expect(screen.getByText('alpha')).toBeInTheDocument()
    expect(screen.getByText('beta')).toBeInTheDocument()
  })

  it('wraps every data row and forwards extra props onto the <tr>', () => {
    const onContextMenu = vi.fn((e: React.MouseEvent) => e.preventDefault())
    render(
      <Table
        columns={columns}
        data={data}
        rowKey={(r) => r.id}
        renderRow={(row, rowElement) =>
          cloneElement(rowElement, {
            'data-testid': `row-${row.id}`,
            onContextMenu,
          } as Partial<React.ComponentPropsWithoutRef<'tr'>>)
        }
      />
    )
    const row = screen.getByTestId('row-1')
    expect(row.tagName).toBe('TR')
    expect(screen.getByTestId('row-2')).toBeInTheDocument()
    fireEvent.contextMenu(row)
    expect(onContextMenu).toHaveBeenCalledTimes(1)
  })

  it('still calls onRowClick on a wrapped row', () => {
    const onRowClick = vi.fn()
    render(
      <Table
        columns={columns}
        data={data}
        rowKey={(r) => r.id}
        onRowClick={onRowClick}
        renderRow={(row, el) =>
          cloneElement(el, {
            'data-testid': `row-${row.id}`,
          } as Partial<React.ComponentPropsWithoutRef<'tr'>>)
        }
      />
    )
    fireEvent.click(screen.getByTestId('row-1'))
    expect(onRowClick).toHaveBeenCalledWith(data[0])
  })
})
```

- [ ] **Step 2: Run test to verify it fails** — `pnpm test -- run src/components/Table` → the wrap tests FAIL (unknown prop `renderRow`, no testid rendered).

- [ ] **Step 3: Implement.** In `src/components/Table/index.tsx`:

Add the exported type near `Column`:

```tsx
export type RenderRow<T extends object> = (
  row: T,
  rowElement: React.ReactElement
) => ReactNode
```

Add to `TableProps<T>` (after `onRowClick`):

```tsx
  /**
   * Wrap each data row. `rowElement` is the row's `<tr>` element, which
   * forwards refs and extra props, so it can back e.g. a Radix
   * `ContextMenuTrigger asChild`. Return `rowElement` (possibly wrapped)
   * to render the row.
   */
  renderRow?: RenderRow<T>
```

Make `RowContainer` ref/prop-forwarding (replace the existing function):

```tsx
type RowContainerProps = {
  onClick?: () => void
} & PropsWithChildrenAndClassName &
  Omit<
    React.ComponentPropsWithoutRef<'tr'>,
    'onClick' | 'className' | 'children'
  >

const RowContainer = forwardRef<HTMLTableRowElement, RowContainerProps>(
  function RowContainer({ className, children, onClick, ...rest }, ref) {
    return (
      <tr
        ref={ref}
        className={cn(
          'hover:bg-muted/50 data-[state=selected]:bg-muted -z-0 [grid-column:1/-1] grid max-w-full [grid-template-columns:subgrid] border-b transition-colors last:border-none',
          onClick && 'cursor-pointer',
          className
        )}
        onClick={onClick}
        {...rest}
      >
        {children}
      </tr>
    )
  }
)
```

Thread the prop: `RowProps<T>` gains `renderRow?: RenderRow<T>`; `Row`'s data variant builds the element and applies the wrapper:

```tsx
  const { row, onClick, columns, className, renderRow } = props
  const rowElement = (
    <RowContainer
      className={className}
      onClick={onClick ? () => onClick(row) : undefined}
    >
      {columns.map((column) => (
        <Cell key={column.key.toString()} column={column} row={row} />
      ))}
    </RowContainer>
  )
  return <>{renderRow ? renderRow(row, rowElement) : rowElement}</>
```

`BodyProps<T>` gains `renderRow?: RenderRow<T>`. In `Body`, rename the local `renderRow` function to `renderBodyRow` (both definition and the `data.map(renderBodyRow)` call) to avoid shadowing, destructure `renderRow` from props, and pass it to `<RowGroup>`, `<RowExpandable>`, and `<Row>`. `RowExpandable` and `RowGroup` each gain a `renderRow?: RenderRow<T>` prop and pass it to their inner `<Row>` (in `RowExpandable`, only the `<Row>` is wrapped — the expanded-content `<div>` stays outside). `TableRoot` passes `renderRow` from its destructured props to `<Table.Body>` (add `renderRow` to the destructuring at the `onRowClick` site and to the Body call).

- [ ] **Step 4: Run tests** — `pnpm test -- run src/components/Table` → PASS (all 3).
- [ ] **Step 5: Type-check and lint** — `pnpm type-check && pnpm lint` → clean.
- [ ] **Step 6: Commit**

```bash
git add src/components/Table/index.tsx src/components/Table/index.test.tsx
git commit -m "feat(Table): add renderRow prop to wrap data rows"
```

### Task 2: Story + verification + draft PR

**Files:**
- Modify: `src/components/Table/index.stories.tsx`

- [ ] **Step 1: Add story** `WithRenderRow` following the file's existing `TableWithState`/`defaultArgs` idiom (reuse the file's mock data): a stateful wrapper with `const [msg, setMsg] = useState('Right-click a row')`, rendering the message above a `<Table {...args} renderRow={(row, el) => cloneElement(el, { onContextMenu: (e) => { e.preventDefault(); setMsg(\`contextmenu on row \${rowLabel(row)}\`) } })} />` where `rowLabel` uses whatever display field the file's mock rows have.
- [ ] **Step 2: Verify** — `pnpm type-check && pnpm lint && pnpm test -- run && pnpm build` → all pass.
- [ ] **Step 3: Commit, push, draft PR**

```bash
git add src/components/Table/index.stories.tsx
git commit -m "docs(Table): add renderRow story"
git push -u origin feat/table-render-row
gh pr create --draft --title "feat(Table): add renderRow prop to wrap data rows" --body "..."
```
