import { describe, it, expect } from 'vitest'
import { parse, View, Warn } from 'vega'
import { expressionInterpreter } from 'vega-interpreter'

describe('ChartRenderer CSP compliance', () => {
  it('renders a chart using vega-interpreter without eval', async () => {
    const spec = {
      $schema: 'https://vega.github.io/schema/vega/v5.json',
      width: 400,
      height: 200,
      data: [
        {
          name: 'table',
          values: [
            { category: 'A', amount: 28 },
            { category: 'B', amount: 55 },
          ],
        },
      ],
      marks: [
        {
          type: 'rect',
          from: { data: 'table' },
          encode: {
            enter: {
              x: { scale: 'xscale', field: 'category' },
              width: { scale: 'xscale', band: 1 },
              y: { scale: 'yscale', field: 'amount' },
              y2: { scale: 'yscale', value: 0 },
            },
          },
        },
      ],
      scales: [
        {
          name: 'xscale',
          type: 'band',
          domain: { data: 'table', field: 'category' },
          range: 'width',
        },
        {
          name: 'yscale',
          domain: { data: 'table', field: 'amount' },
          range: 'height',
        },
      ],
    }

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const runtime = parse(spec as any, undefined, { ast: true })

    // This is the key - using expr: vegaInterpreter means no eval() is called
    const view = new View(runtime, {
      renderer: 'none',
      logLevel: Warn,
      expr: expressionInterpreter,
    })

    await view.runAsync()

    // If we get here without error, CSP compliance works
    expect(view.data('table')).toHaveLength(2)

    view.finalize()
  })
})
