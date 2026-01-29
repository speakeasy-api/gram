'use client'

import {
  createContext,
  useContext,
  useState,
  useCallback,
  useMemo,
  type ReactNode,
  type FC,
} from 'react'
import type {
  SubAgentState,
  SubAgentEvent,
  AgentExecutionTree,
  SubAgentMessage,
  AgentsConfig,
} from '@/types/agents'

/** Regex to match complete GRAM_AGENT markers in content */
const AGENT_MARKER_REGEX = /<!--GRAM_AGENT:(.*?)-->/gs

/**
 * Parse agent events from content that contains GRAM_AGENT markers.
 * Returns the cleaned content (without markers) and any extracted events.
 * Handles streaming by also returning any trailing partial marker.
 */
export function parseAgentEventsFromContent(content: string): {
  cleanContent: string
  events: SubAgentEvent[]
  /** Trailing content that might be a partial marker (for buffering) */
  trailingPartial: string
} {
  const events: SubAgentEvent[] = []

  // First, extract all complete markers
  let cleanContent = content.replace(AGENT_MARKER_REGEX, (_, jsonStr) => {
    try {
      const event = JSON.parse(jsonStr) as SubAgentEvent
      events.push(event)
    } catch (e) {
      console.warn('Failed to parse agent event:', e)
    }
    return '' // Remove the marker from content
  })

  // Check for a trailing partial marker (started but not closed)
  // This handles streaming where markers might be split across chunks
  let trailingPartial = ''

  // First check for a complete marker prefix that's not closed
  const lastMarkerStart = cleanContent.lastIndexOf('<!--GRAM_AGENT:')
  if (lastMarkerStart !== -1) {
    // Check if there's a closing --> after this start
    const afterStart = cleanContent.slice(lastMarkerStart)
    if (!afterStart.includes('-->')) {
      // This is a partial marker, buffer it
      trailingPartial = afterStart
      cleanContent = cleanContent.slice(0, lastMarkerStart)
    }
  }

  // Also check for partial marker prefix at the end of content
  // smoothStream might split in the middle of "<!--GRAM_AGENT:"
  // We need to buffer any trailing content that could be the start of a marker
  if (!trailingPartial) {
    const MARKER_PREFIX = '<!--GRAM_AGENT:'
    // Check longest prefixes first to find the best match
    for (let i = MARKER_PREFIX.length; i >= 1; i--) {
      const possiblePrefix = MARKER_PREFIX.slice(0, i)
      if (cleanContent.endsWith(possiblePrefix)) {
        trailingPartial = possiblePrefix
        cleanContent = cleanContent.slice(0, -possiblePrefix.length)
        break
      }
    }
  }

  return { cleanContent, events, trailingPartial }
}

/**
 * Generate a unique key for an event to prevent duplicate processing.
 */
export function getEventKey(event: SubAgentEvent): string {
  switch (event.type) {
    case 'sub_agent.spawn':
      return `spawn:${event.agent_id}`
    case 'sub_agent.delta':
      // For deltas, we use agent_id + content hash to dedupe
      return `delta:${event.agent_id}:${event.content.length}:${event.content.slice(0, 50)}`
    case 'sub_agent.tool_call':
      return `tool_call:${event.agent_id}:${event.tool_call_id}`
    case 'sub_agent.tool_result':
      return `tool_result:${event.agent_id}:${event.tool_call_id}`
    case 'sub_agent.complete':
      return `complete:${event.agent_id}`
    default:
      return `unknown:${JSON.stringify(event)}`
  }
}

interface SubAgentContextValue {
  /** The current execution tree containing all agents */
  executionTree: AgentExecutionTree
  /** Set of agent IDs that are currently expanded in the UI */
  expandedAgents: Set<string>
  /** Toggle the expanded state of an agent */
  toggleAgentExpanded: (agentId: string) => void
  /** Get an agent by ID */
  getAgent: (agentId: string) => SubAgentState | undefined
  /** Check if an agent is expanded */
  isAgentExpanded: (agentId: string) => boolean
  /** Get all root-level agents (no parent) */
  getRootAgents: () => SubAgentState[]
  /** Get children of an agent */
  getChildAgents: (agentId: string) => SubAgentState[]
  /** Process an incoming sub-agent event */
  handleEvent: (event: SubAgentEvent) => void
  /** Process content that may contain embedded agent events, returns cleaned content */
  processContent: (content: string) => string
  /** Clear all agent state */
  clearAgents: () => void
  /** Agents configuration */
  config: AgentsConfig
}

const SubAgentContext = createContext<SubAgentContextValue | null>(null)

interface SubAgentProviderProps {
  children: ReactNode
  config?: AgentsConfig
  /** Initial agents for testing/storybook */
  initialAgents?: SubAgentState[]
}

function createEmptyTree(): AgentExecutionTree {
  return {
    agents: new Map(),
    rootAgentIds: [],
  }
}

function initializeTree(initialAgents?: SubAgentState[]): AgentExecutionTree {
  if (!initialAgents || initialAgents.length === 0) {
    return createEmptyTree()
  }

  const agents = new Map<string, SubAgentState>()
  const rootAgentIds: string[] = []

  for (const agent of initialAgents) {
    agents.set(agent.id, agent)
    if (!agent.parentId) {
      rootAgentIds.push(agent.id)
    }
  }

  return { agents, rootAgentIds }
}

export const SubAgentProvider: FC<SubAgentProviderProps> = ({
  children,
  config = {},
  initialAgents,
}) => {
  const [executionTree, setExecutionTree] = useState<AgentExecutionTree>(() =>
    initializeTree(initialAgents)
  )
  const [expandedAgents, setExpandedAgents] = useState<Set<string>>(() => {
    // Auto-expand if configured
    if (config.autoExpandSubAgents && initialAgents) {
      return new Set(initialAgents.map((a) => a.id))
    }
    return new Set()
  })

  const toggleAgentExpanded = useCallback((agentId: string) => {
    setExpandedAgents((prev) => {
      const next = new Set(prev)
      if (next.has(agentId)) {
        next.delete(agentId)
      } else {
        next.add(agentId)
      }
      return next
    })
  }, [])

  const getAgent = useCallback(
    (agentId: string) => {
      return executionTree.agents.get(agentId)
    },
    [executionTree]
  )

  const isAgentExpanded = useCallback(
    (agentId: string) => {
      return expandedAgents.has(agentId)
    },
    [expandedAgents]
  )

  const getRootAgents = useCallback(() => {
    return executionTree.rootAgentIds
      .map((id) => executionTree.agents.get(id))
      .filter((agent): agent is SubAgentState => agent !== undefined)
  }, [executionTree])

  const getChildAgents = useCallback(
    (agentId: string) => {
      const agent = executionTree.agents.get(agentId)
      if (!agent) return []
      return agent.children
        .map((id) => executionTree.agents.get(id))
        .filter((child): child is SubAgentState => child !== undefined)
    },
    [executionTree]
  )

  const handleEvent = useCallback(
    (event: SubAgentEvent) => {
      setExecutionTree((prev) => {
        const agents = new Map(prev.agents)
        const rootAgentIds = [...prev.rootAgentIds]

        switch (event.type) {
          case 'sub_agent.spawn': {
            const newAgent: SubAgentState = {
              id: event.agent_id,
              parentId: event.parent_id ?? null,
              name: event.name,
              description: event.description,
              task: event.task,
              status: 'running',
              messages: [],
              children: [],
              startedAt: new Date(),
            }
            agents.set(event.agent_id, newAgent)

            // Add to parent's children or root list
            if (event.parent_id) {
              const parent = agents.get(event.parent_id)
              if (parent) {
                agents.set(event.parent_id, {
                  ...parent,
                  children: [...parent.children, event.agent_id],
                })
              }
            } else {
              rootAgentIds.push(event.agent_id)
            }

            // Auto-expand if configured
            if (config.autoExpandSubAgents) {
              setExpandedAgents((prev) => new Set([...prev, event.agent_id]))
            }
            break
          }

          case 'sub_agent.delta': {
            const agent = agents.get(event.agent_id)
            if (agent) {
              const lastMessage = agent.messages[agent.messages.length - 1]
              if (lastMessage && lastMessage.role === 'assistant') {
                // Append to existing message
                const updatedMessages = [...agent.messages]
                updatedMessages[updatedMessages.length - 1] = {
                  ...lastMessage,
                  content: lastMessage.content + event.content,
                }
                agents.set(event.agent_id, {
                  ...agent,
                  messages: updatedMessages,
                })
              } else {
                // Create new message
                const newMessage: SubAgentMessage = {
                  id: `msg-${Date.now()}`,
                  role: 'assistant',
                  content: event.content,
                  timestamp: new Date(),
                }
                agents.set(event.agent_id, {
                  ...agent,
                  messages: [...agent.messages, newMessage],
                })
              }
            }
            break
          }

          case 'sub_agent.tool_call': {
            const agent = agents.get(event.agent_id)
            if (agent) {
              const newMessage: SubAgentMessage = {
                id: event.tool_call_id,
                role: 'tool',
                content: `Calling ${event.tool_name}...`,
                toolName: event.tool_name,
                toolCallId: event.tool_call_id,
                timestamp: new Date(),
              }
              agents.set(event.agent_id, {
                ...agent,
                messages: [...agent.messages, newMessage],
              })
            }
            break
          }

          case 'sub_agent.tool_result': {
            const agent = agents.get(event.agent_id)
            if (agent) {
              // Update the tool call message with the result
              const updatedMessages = agent.messages.map((msg) => {
                if (msg.toolCallId === event.tool_call_id) {
                  return {
                    ...msg,
                    content: event.is_error
                      ? `Error: ${event.result}`
                      : event.result,
                  }
                }
                return msg
              })
              agents.set(event.agent_id, {
                ...agent,
                messages: updatedMessages,
              })
            }
            break
          }

          case 'sub_agent.complete': {
            const agent = agents.get(event.agent_id)
            if (agent) {
              agents.set(event.agent_id, {
                ...agent,
                status: event.status,
                result: event.result,
                error: event.error,
                completedAt: new Date(),
              })
            }
            break
          }
        }

        return { agents, rootAgentIds }
      })
    },
    [config.autoExpandSubAgents]
  )

  const clearAgents = useCallback(() => {
    setExecutionTree(createEmptyTree())
    setExpandedAgents(new Set())
  }, [])

  // Process content that may contain embedded agent events
  // Returns the cleaned content (without markers)
  const processContent = useCallback(
    (content: string): string => {
      const { cleanContent, events } = parseAgentEventsFromContent(content)
      // Process each extracted event
      for (const event of events) {
        handleEvent(event)
      }
      return cleanContent
    },
    [handleEvent]
  )

  const value = useMemo(
    () => ({
      executionTree,
      expandedAgents,
      toggleAgentExpanded,
      getAgent,
      isAgentExpanded,
      getRootAgents,
      getChildAgents,
      handleEvent,
      processContent,
      clearAgents,
      config,
    }),
    [
      executionTree,
      expandedAgents,
      toggleAgentExpanded,
      getAgent,
      isAgentExpanded,
      getRootAgents,
      getChildAgents,
      handleEvent,
      processContent,
      clearAgents,
      config,
    ]
  )

  return (
    <SubAgentContext.Provider value={value}>
      {children}
    </SubAgentContext.Provider>
  )
}

export const useSubAgent = () => {
  const context = useContext(SubAgentContext)
  if (!context) {
    throw new Error('useSubAgent must be used within a SubAgentProvider')
  }
  return context
}

/**
 * Optional hook that returns null if not within a SubAgentProvider.
 * Useful for components that may or may not be used with agents.
 */
export const useSubAgentOptional = () => {
  return useContext(SubAgentContext)
}
