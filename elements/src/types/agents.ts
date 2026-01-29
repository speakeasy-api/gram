/**
 * Sub-agent message within an agent's execution
 */
export interface SubAgentMessage {
  id: string
  role: 'assistant' | 'tool'
  content: string
  toolName?: string
  toolCallId?: string
  timestamp: Date
}

/**
 * Status of a sub-agent execution
 */
export type SubAgentStatus = 'pending' | 'running' | 'completed' | 'failed'

/**
 * State of an individual sub-agent
 */
export interface SubAgentState {
  /** Unique identifier for this sub-agent instance */
  id: string
  /** Parent agent ID, null for root-level agents */
  parentId: string | null
  /** Display name of the agent */
  name: string
  /** Description of what this agent does */
  description?: string
  /** The specific task assigned to this agent */
  task: string
  /** Current execution status */
  status: SubAgentStatus
  /** Messages generated during agent execution */
  messages: SubAgentMessage[]
  /** IDs of child agents spawned by this agent */
  children: string[]
  /** Final result when completed */
  result?: string
  /** Error message when failed */
  error?: string
  /** When the agent started */
  startedAt: Date
  /** When the agent completed (success or failure) */
  completedAt?: Date
}

/**
 * Tree structure tracking all sub-agents in an execution
 */
export interface AgentExecutionTree {
  /** Map of agent ID to agent state */
  agents: Map<string, SubAgentState>
  /** Root-level agent IDs (agents with no parent) */
  rootAgentIds: string[]
}

/**
 * SSE event types for sub-agent streaming
 */
export type SubAgentEventType =
  | 'sub_agent.spawn'
  | 'sub_agent.delta'
  | 'sub_agent.tool_call'
  | 'sub_agent.tool_result'
  | 'sub_agent.complete'

/**
 * Base interface for all sub-agent SSE events
 * Note: Field names match the snake_case JSON from the Go server
 */
export interface SubAgentEventBase {
  type: SubAgentEventType
  agent_id: string
}

/**
 * Event when a new sub-agent is spawned
 */
export interface SubAgentSpawnEvent extends SubAgentEventBase {
  type: 'sub_agent.spawn'
  parent_id?: string | null
  name: string
  description?: string
  task: string
}

/**
 * Event for streaming text content from a sub-agent
 */
export interface SubAgentDeltaEvent extends SubAgentEventBase {
  type: 'sub_agent.delta'
  content: string
}

/**
 * Event when a sub-agent makes a tool call
 */
export interface SubAgentToolCallEvent extends SubAgentEventBase {
  type: 'sub_agent.tool_call'
  tool_call_id: string
  tool_name: string
  args: Record<string, unknown>
}

/**
 * Event when a tool call returns a result
 */
export interface SubAgentToolResultEvent extends SubAgentEventBase {
  type: 'sub_agent.tool_result'
  tool_call_id: string
  tool_name: string
  result: string
  is_error?: boolean
}

/**
 * Event when a sub-agent completes execution
 */
export interface SubAgentCompleteEvent extends SubAgentEventBase {
  type: 'sub_agent.complete'
  status: 'completed' | 'failed'
  result?: string
  error?: string
}

/**
 * Union type of all sub-agent events
 */
export type SubAgentEvent =
  | SubAgentSpawnEvent
  | SubAgentDeltaEvent
  | SubAgentToolCallEvent
  | SubAgentToolResultEvent
  | SubAgentCompleteEvent

/**
 * Configuration for agentic workflows in Elements
 */
export interface AgentsConfig {
  /**
   * Enable sub-agent spawning capability
   * @default false
   */
  enabled?: boolean

  /**
   * Whether to auto-expand sub-agent views when they spawn
   * @default false
   */
  autoExpandSubAgents?: boolean
}
