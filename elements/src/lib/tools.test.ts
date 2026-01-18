import { beforeEach, describe, expect, it, vi } from 'vitest'
import z from 'zod'
import type { Tool as AssistantTool } from '@assistant-ui/react'
import type { ToolsRequiringApproval } from '@/types'
import {
  toAISDKTools,
  getEnabledTools,
  defineFrontendTool,
  setFrontendToolApprovalConfig,
  clearFrontendToolApprovalConfig,
  wrapToolsWithApproval,
  type ApprovalHelpers,
} from './tools'

// Mock makeAssistantTool since it's from @assistant-ui/react
vi.mock('@assistant-ui/react', () => ({
  makeAssistantTool: vi.fn((props) => {
    const Component = () => null
    Component.unstable_tool = props
    return Component
  }),
}))

describe('toAISDKTools', () => {
  it('converts tools with zod parameters to JSON schema', () => {
    const tools: Record<string, AssistantTool> = {
      greet: {
        description: 'Greet a user',
        parameters: z.object({
          name: z.string().describe('The name to greet'),
        }),
      },
    }

    const result = toAISDKTools(tools)

    expect(result).toHaveProperty('greet')
    expect(result.greet.description).toBe('Greet a user')
    expect(result.greet.parameters).toMatchObject({
      type: 'object',
      properties: {
        name: { type: 'string', description: 'The name to greet' },
      },
      required: ['name'],
    })
  })

  it('converts tools with plain object parameters', () => {
    const tools: Record<string, AssistantTool> = {
      calculate: {
        description: 'Calculate a sum',
        parameters: {
          type: 'object',
          properties: {
            a: { type: 'number' },
            b: { type: 'number' },
          },
          required: ['a', 'b'],
        },
      },
    }

    const result = toAISDKTools(tools)

    expect(result.calculate.parameters).toMatchObject({
      type: 'object',
      properties: {
        a: { type: 'number' },
        b: { type: 'number' },
      },
    })
  })

  it('handles tools without description', () => {
    const tools: Record<string, AssistantTool> = {
      noDesc: {
        parameters: z.object({ value: z.string() }),
      },
    }

    const result = toAISDKTools(tools)

    expect(result.noDesc.description).toBeUndefined()
  })

  it('handles empty tools object', () => {
    const result = toAISDKTools({})
    expect(result).toEqual({})
  })

  it('converts multiple tools', () => {
    const tools: Record<string, AssistantTool> = {
      toolA: {
        description: 'Tool A',
        parameters: z.object({ x: z.number() }),
      },
      toolB: {
        description: 'Tool B',
        parameters: z.object({ y: z.string() }),
      },
    }

    const result = toAISDKTools(tools)

    expect(Object.keys(result)).toEqual(['toolA', 'toolB'])
    expect(result.toolA.description).toBe('Tool A')
    expect(result.toolB.description).toBe('Tool B')
  })
})

describe('getEnabledTools', () => {
  it('returns only enabled frontend tools', () => {
    const tools: Record<string, AssistantTool> = {
      enabled: {
        description: 'Enabled tool',
        parameters: z.object({}),
        disabled: false,
      },
      disabled: {
        description: 'Disabled tool',
        parameters: z.object({}),
        disabled: true,
      },
    }

    const result = getEnabledTools(tools)

    expect(result).toHaveProperty('enabled')
    expect(result).not.toHaveProperty('disabled')
  })

  it('filters out backend tools', () => {
    const tools = {
      frontend: {
        description: 'Frontend tool',
        parameters: z.object({}),
        type: 'frontend' as const,
      },
      backend: {
        description: 'Backend tool',
        parameters: z.object({}),
        type: 'backend' as const,
      },
    }

    // Cast to bypass the type check since we're testing the runtime behavior
    const result = getEnabledTools(
      tools as unknown as Record<string, AssistantTool>
    )

    expect(result).toHaveProperty('frontend')
    expect(result).not.toHaveProperty('backend')
  })

  it('includes tools without disabled or type property', () => {
    const tools: Record<string, AssistantTool> = {
      plain: {
        description: 'Plain tool',
        parameters: z.object({}),
      },
    }

    const result = getEnabledTools(tools)

    expect(result).toHaveProperty('plain')
  })

  it('returns empty object for empty input', () => {
    const result = getEnabledTools({})
    expect(result).toEqual({})
  })
})

describe('setFrontendToolApprovalConfig / clearFrontendToolApprovalConfig', () => {
  beforeEach(() => {
    clearFrontendToolApprovalConfig()
  })

  it('sets and clears approval config', async () => {
    const helpers: ApprovalHelpers = {
      requestApproval: vi.fn().mockResolvedValue(true),
      isToolApproved: vi.fn().mockReturnValue(false),
      whitelistTool: vi.fn(),
    }

    setFrontendToolApprovalConfig(helpers, ['testTool'])
    clearFrontendToolApprovalConfig()

    // After clearing, the config should be null (tested indirectly via defineFrontendTool)
  })
})

describe('defineFrontendTool', () => {
  beforeEach(() => {
    clearFrontendToolApprovalConfig()
  })

  it('creates a frontend tool component', () => {
    const tool: AssistantTool = {
      description: 'Test tool',
      parameters: z.object({ input: z.string() }),
      execute: vi.fn().mockResolvedValue('result'),
    }

    const FrontendTool = defineFrontendTool(tool, 'testTool')

    expect(FrontendTool).toBeDefined()
    expect(FrontendTool.unstable_tool).toBeDefined()
    expect(FrontendTool.unstable_tool.toolName).toBe('testTool')
  })

  it('executes tool without approval when no config is set', async () => {
    const executeMock = vi.fn().mockResolvedValue('result')
    const tool: AssistantTool = {
      description: 'Test tool',
      parameters: z.object({ input: z.string() }),
      execute: executeMock,
    }

    const FrontendTool = defineFrontendTool(tool, 'testTool')
    const context = {
      toolCallId: 'call-123',
      abortSignal: new AbortController().signal,
      human: vi.fn(),
    }

    const result = await FrontendTool.unstable_tool.execute?.(
      { input: 'test' },
      context
    )

    expect(executeMock).toHaveBeenCalledWith({ input: 'test' }, context)
    expect(result).toBe('result')
  })

  it('requests approval when tool requires it', async () => {
    const executeMock = vi.fn().mockResolvedValue('result')
    const requestApprovalMock = vi.fn().mockResolvedValue(true)

    const helpers: ApprovalHelpers = {
      requestApproval: requestApprovalMock,
      isToolApproved: vi.fn().mockReturnValue(false),
      whitelistTool: vi.fn(),
    }

    setFrontendToolApprovalConfig(helpers, ['testTool'])

    const tool: AssistantTool = {
      description: 'Test tool',
      parameters: z.object({ input: z.string() }),
      execute: executeMock,
    }

    const FrontendTool = defineFrontendTool(tool, 'testTool')
    const context = {
      toolCallId: 'call-123',
      abortSignal: new AbortController().signal,
      human: vi.fn(),
    }

    await FrontendTool.unstable_tool.execute?.({ input: 'test' }, context)

    expect(requestApprovalMock).toHaveBeenCalledWith('testTool', 'call-123', {
      input: 'test',
    })
    expect(executeMock).toHaveBeenCalled()
  })

  it('returns error when approval is denied', async () => {
    const executeMock = vi.fn().mockResolvedValue('result')

    const helpers: ApprovalHelpers = {
      requestApproval: vi.fn().mockResolvedValue(false),
      isToolApproved: vi.fn().mockReturnValue(false),
      whitelistTool: vi.fn(),
    }

    setFrontendToolApprovalConfig(helpers, ['testTool'])

    const tool: AssistantTool = {
      description: 'Test tool',
      parameters: z.object({ input: z.string() }),
      execute: executeMock,
    }

    const FrontendTool = defineFrontendTool(tool, 'testTool')
    const context = {
      toolCallId: 'call-123',
      abortSignal: new AbortController().signal,
      human: vi.fn(),
    }

    const result = await FrontendTool.unstable_tool.execute?.(
      { input: 'test' },
      context
    )

    expect(executeMock).not.toHaveBeenCalled()
    expect(result).toMatchObject({
      isError: true,
      content: expect.arrayContaining([
        expect.objectContaining({
          type: 'text',
          text: expect.stringContaining('denied'),
        }),
      ]),
    })
  })

  it('skips approval when tool is already approved', async () => {
    const executeMock = vi.fn().mockResolvedValue('result')
    const requestApprovalMock = vi.fn()

    const helpers: ApprovalHelpers = {
      requestApproval: requestApprovalMock,
      isToolApproved: vi.fn().mockReturnValue(true),
      whitelistTool: vi.fn(),
    }

    setFrontendToolApprovalConfig(helpers, ['testTool'])

    const tool: AssistantTool = {
      description: 'Test tool',
      parameters: z.object({ input: z.string() }),
      execute: executeMock,
    }

    const FrontendTool = defineFrontendTool(tool, 'testTool')
    const context = {
      toolCallId: 'call-123',
      abortSignal: new AbortController().signal,
      human: vi.fn(),
    }

    await FrontendTool.unstable_tool.execute?.({ input: 'test' }, context)

    expect(requestApprovalMock).not.toHaveBeenCalled()
    expect(executeMock).toHaveBeenCalled()
  })

  it('does not request approval for tools not in approval list', async () => {
    const executeMock = vi.fn().mockResolvedValue('result')
    const requestApprovalMock = vi.fn()

    const helpers: ApprovalHelpers = {
      requestApproval: requestApprovalMock,
      isToolApproved: vi.fn().mockReturnValue(false),
      whitelistTool: vi.fn(),
    }

    setFrontendToolApprovalConfig(helpers, ['otherTool'])

    const tool: AssistantTool = {
      description: 'Test tool',
      parameters: z.object({ input: z.string() }),
      execute: executeMock,
    }

    const FrontendTool = defineFrontendTool(tool, 'testTool')
    const context = {
      toolCallId: 'call-123',
      abortSignal: new AbortController().signal,
      human: vi.fn(),
    }

    await FrontendTool.unstable_tool.execute?.({ input: 'test' }, context)

    expect(requestApprovalMock).not.toHaveBeenCalled()
    expect(executeMock).toHaveBeenCalled()
  })

  it('supports function-based approval config', async () => {
    const executeMock = vi.fn().mockResolvedValue('result')
    const requestApprovalMock = vi.fn().mockResolvedValue(true)

    const helpers: ApprovalHelpers = {
      requestApproval: requestApprovalMock,
      isToolApproved: vi.fn().mockReturnValue(false),
      whitelistTool: vi.fn(),
    }

    const requiresApprovalFn: ToolsRequiringApproval = ({ toolName }) =>
      toolName.startsWith('protected_')

    setFrontendToolApprovalConfig(helpers, requiresApprovalFn)

    const tool: AssistantTool = {
      description: 'Protected tool',
      parameters: z.object({ input: z.string() }),
      execute: executeMock,
    }

    const FrontendTool = defineFrontendTool(tool, 'protected_action')
    const context = {
      toolCallId: 'call-123',
      abortSignal: new AbortController().signal,
      human: vi.fn(),
    }

    await FrontendTool.unstable_tool.execute?.({ input: 'test' }, context)

    expect(requestApprovalMock).toHaveBeenCalled()
  })
})

describe('wrapToolsWithApproval', () => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  type AnyToolSet = Record<string, any>
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  type AnyFn = (...args: any[]) => any

  const createMockHelpers = (
    overrides: Partial<ApprovalHelpers> = {}
  ): ApprovalHelpers => ({
    requestApproval: vi.fn().mockResolvedValue(true),
    isToolApproved: vi.fn().mockReturnValue(false),
    whitelistTool: vi.fn(),
    ...overrides,
  })

  const createMockTool = (
    executeMock = vi.fn().mockResolvedValue('result')
  ) => ({
    description: 'Test',
    execute: executeMock,
  })

  // Helper to call execute without TS complaining about the options shape
  const callExecute = async (
    tool: { execute?: AnyFn },
    args: unknown,
    options: unknown
  ) => {
    return tool.execute?.(args, options)
  }

  it('returns tools unchanged when no approval config', () => {
    const tools: AnyToolSet = {
      testTool: createMockTool(),
    }

    const result = wrapToolsWithApproval(tools, undefined, createMockHelpers())

    expect(result).toBe(tools)
  })

  it('returns tools unchanged for empty approval array', () => {
    const tools: AnyToolSet = {
      testTool: createMockTool(),
    }

    const result = wrapToolsWithApproval(tools, [], createMockHelpers())

    expect(result).toBe(tools)
  })

  it('wraps tools that require approval', async () => {
    const executeMock = vi.fn().mockResolvedValue('result')
    const tools: AnyToolSet = {
      sensitiveAction: {
        description: 'Sensitive action',
        execute: executeMock,
      },
    }

    const helpers = createMockHelpers()
    const result = wrapToolsWithApproval(tools, ['sensitiveAction'], helpers)

    await callExecute(result.sensitiveAction, {}, { toolCallId: 'call-123' })

    expect(helpers.requestApproval).toHaveBeenCalledWith(
      'sensitiveAction',
      'call-123',
      {}
    )
    expect(executeMock).toHaveBeenCalled()
  })

  it('does not wrap tools not requiring approval', async () => {
    const executeMock = vi.fn().mockResolvedValue('result')
    const tools: AnyToolSet = {
      safeAction: {
        description: 'Safe action',
        execute: executeMock,
      },
    }

    const helpers = createMockHelpers()
    const result = wrapToolsWithApproval(tools, ['otherTool'], helpers)

    await callExecute(result.safeAction, {}, {})

    expect(helpers.requestApproval).not.toHaveBeenCalled()
    expect(executeMock).toHaveBeenCalled()
  })

  it('skips approval for already approved tools', async () => {
    const executeMock = vi.fn().mockResolvedValue('result')
    const tools: AnyToolSet = {
      testTool: {
        description: 'Test',
        execute: executeMock,
      },
    }

    const helpers = createMockHelpers({
      isToolApproved: vi.fn().mockReturnValue(true),
    })
    const result = wrapToolsWithApproval(tools, ['testTool'], helpers)

    await callExecute(result.testTool, {}, {})

    expect(helpers.requestApproval).not.toHaveBeenCalled()
    expect(executeMock).toHaveBeenCalled()
  })

  it('returns error when approval denied', async () => {
    const executeMock = vi.fn().mockResolvedValue('result')
    const tools: AnyToolSet = {
      testTool: {
        description: 'Test',
        execute: executeMock,
      },
    }

    const helpers = createMockHelpers({
      requestApproval: vi.fn().mockResolvedValue(false),
    })
    const result = wrapToolsWithApproval(tools, ['testTool'], helpers)

    const execResult = await callExecute(result.testTool, {}, {})

    expect(executeMock).not.toHaveBeenCalled()
    expect(execResult).toMatchObject({
      isError: true,
      content: expect.arrayContaining([
        expect.objectContaining({
          type: 'text',
          text: expect.stringContaining('denied'),
        }),
      ]),
    })
  })

  it('handles tools without execute function', () => {
    const tools: AnyToolSet = {
      noExecute: {
        description: 'No execute',
      },
    }

    const result = wrapToolsWithApproval(
      tools,
      ['noExecute'],
      createMockHelpers()
    )

    expect(result.noExecute.execute).toBeUndefined()
  })

  it('supports function-based approval config', async () => {
    const executeMock = vi.fn().mockResolvedValue('result')
    const tools: AnyToolSet = {
      protected_delete: {
        description: 'Delete something',
        execute: executeMock,
      },
      safe_read: {
        description: 'Read something',
        execute: vi.fn().mockResolvedValue('read result'),
      },
    }

    const requiresApprovalFn: ToolsRequiringApproval = ({ toolName }) =>
      toolName.startsWith('protected_')

    const helpers = createMockHelpers()
    const result = wrapToolsWithApproval(tools, requiresApprovalFn, helpers)

    await callExecute(result.protected_delete, {}, { toolCallId: 'call-1' })
    await callExecute(result.safe_read, {}, { toolCallId: 'call-2' })

    expect(helpers.requestApproval).toHaveBeenCalledTimes(1)
    expect(helpers.requestApproval).toHaveBeenCalledWith(
      'protected_delete',
      'call-1',
      {}
    )
  })

  it('handles multiple tools with mixed approval requirements', async () => {
    const tools: AnyToolSet = {
      requiresApproval1: {
        description: 'Tool 1',
        execute: vi.fn().mockResolvedValue('result1'),
      },
      requiresApproval2: {
        description: 'Tool 2',
        execute: vi.fn().mockResolvedValue('result2'),
      },
      noApproval: {
        description: 'Tool 3',
        execute: vi.fn().mockResolvedValue('result3'),
      },
    }

    const helpers = createMockHelpers()
    const result = wrapToolsWithApproval(
      tools,
      ['requiresApproval1', 'requiresApproval2'],
      helpers
    )

    await callExecute(result.requiresApproval1, {}, { toolCallId: 'call-1' })
    await callExecute(result.noApproval, {}, { toolCallId: 'call-2' })

    expect(helpers.requestApproval).toHaveBeenCalledTimes(1)
  })

  it('handles missing toolCallId gracefully', async () => {
    const executeMock = vi.fn().mockResolvedValue('result')
    const tools: AnyToolSet = {
      testTool: {
        description: 'Test',
        execute: executeMock,
      },
    }

    const helpers = createMockHelpers()
    const result = wrapToolsWithApproval(tools, ['testTool'], helpers)

    // Call without toolCallId in options
    await callExecute(result.testTool, {}, {})

    expect(helpers.requestApproval).toHaveBeenCalledWith('testTool', '', {})
  })
})
