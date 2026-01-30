import type { StoryFn } from '@storybook/react-vite'
import { Chat } from '@/components/Chat'
import { Replay } from '@/components/Replay'
import type { Cassette } from '@/lib/cassette'

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const meta: any = {
  title: 'Replay',
  component: Replay,
  parameters: {
    layout: 'fullscreen',
    // Skip the global ElementsProvider — Replay is its own provider
    elements: { skipProvider: true },
  },
}

export default meta

type Story = StoryFn<typeof Replay>

// ---------------------------------------------------------------------------
// Sample cassettes
// ---------------------------------------------------------------------------

const textOnlyCassette: Cassette = {
  messages: [
    {
      role: 'user',
      content: [{ type: 'text', text: 'What can you help me with?' }],
    },
    {
      role: 'assistant',
      content: [
        {
          type: 'text',
          text: "I can help you with a wide range of tasks! Here are a few things I'm great at:\n\n- **Answering questions** about virtually any topic\n- **Writing and editing** emails, documents, and code\n- **Brainstorming ideas** for projects, names, or strategies\n- **Explaining concepts** in simple terms\n- **Analyzing data** and summarizing information\n\nWhat would you like to explore?",
        },
      ],
    },
    {
      role: 'user',
      content: [{ type: 'text', text: 'Tell me about the Speakeasy SDK.' }],
    },
    {
      role: 'assistant',
      content: [
        {
          type: 'text',
          text: 'Speakeasy generates **type-safe, idiomatic SDKs** from your OpenAPI spec. You get production-ready client libraries in TypeScript, Python, Go, and more — complete with authentication, pagination, and retries built in.\n\nThe generated SDKs stay in sync with your API automatically through CI/CD integration, so your users always have an up-to-date client library. No more hand-writing API wrappers!',
        },
      ],
    },
  ],
}

const toolCallCassette: Cassette = {
  messages: [
    {
      role: 'user',
      content: [
        { type: 'text', text: "What's the weather like in San Francisco?" },
      ],
    },
    {
      role: 'assistant',
      content: [
        {
          type: 'text',
          text: 'Let me check the current weather in San Francisco for you.',
        },
        {
          type: 'tool-call',
          toolCallId: 'tc_weather_1',
          toolName: 'getWeather',
          args: { city: 'San Francisco', units: 'fahrenheit' },
          result: {
            temperature: 68,
            conditions: 'Partly cloudy',
            humidity: 72,
            wind: '12 mph NW',
          },
        },
        {
          type: 'text',
          text: "It's currently **68°F** in San Francisco with partly cloudy skies. The humidity is at 72% with northwest winds at 12 mph. A beautiful day to be outside!",
        },
      ],
    },
    {
      role: 'user',
      content: [{ type: 'text', text: 'How about New York?' }],
    },
    {
      role: 'assistant',
      content: [
        {
          type: 'tool-call',
          toolCallId: 'tc_weather_2',
          toolName: 'getWeather',
          args: { city: 'New York', units: 'fahrenheit' },
          result: {
            temperature: 45,
            conditions: 'Rainy',
            humidity: 89,
            wind: '8 mph E',
          },
        },
        {
          type: 'text',
          text: "New York is quite different — it's **45°F** and rainy with high humidity at 89%. You'd want an umbrella and a warm coat today!",
        },
      ],
    },
  ],
}

const reasoningCassette: Cassette = {
  messages: [
    {
      role: 'user',
      content: [
        {
          type: 'text',
          text: 'If I have 3 boxes, each containing 2 red balls and 1 blue ball, what is the probability of drawing a red ball from each box?',
        },
      ],
    },
    {
      role: 'assistant',
      content: [
        {
          type: 'reasoning',
          text: 'The user wants to find the probability of drawing a red ball from each of the 3 boxes.\n\nEach box has 3 balls total: 2 red and 1 blue.\nProbability of red from one box = 2/3.\nSince the draws are independent, I multiply the probabilities:\nP(all red) = (2/3) × (2/3) × (2/3) = 8/27 ≈ 0.296',
        },
        {
          type: 'text',
          text: "Each box contains 3 balls (2 red, 1 blue), so the probability of drawing red from a single box is **2/3**.\n\nSince you're drawing independently from each box, you multiply the probabilities:\n\n$$P(\\text{all red}) = \\frac{2}{3} \\times \\frac{2}{3} \\times \\frac{2}{3} = \\frac{8}{27} \\approx 29.6\\%$$\n\nSo there's roughly a **29.6% chance** of drawing a red ball from every box.",
        },
      ],
    },
  ],
}

const multiTurnCassette: Cassette = {
  messages: [
    {
      role: 'user',
      content: [
        { type: 'text', text: 'Help me plan a REST API for a todo app.' },
      ],
    },
    {
      role: 'assistant',
      content: [
        {
          type: 'text',
          text: "Here's a clean REST API design for a todo app:\n\n| Method | Endpoint | Description |\n|--------|----------|-------------|\n| `GET` | `/todos` | List all todos |\n| `POST` | `/todos` | Create a new todo |\n| `GET` | `/todos/:id` | Get a single todo |\n| `PATCH` | `/todos/:id` | Update a todo |\n| `DELETE` | `/todos/:id` | Delete a todo |\n\nWant me to define the request/response schemas?",
        },
      ],
    },
    {
      role: 'user',
      content: [
        { type: 'text', text: 'Yes, show me the schema for creating a todo.' },
      ],
    },
    {
      role: 'assistant',
      content: [
        {
          type: 'text',
          text: '**POST `/todos`**\n\n**Request body:**\n```json\n{\n  "title": "Buy groceries",\n  "description": "Milk, eggs, bread",\n  "dueDate": "2025-03-15T10:00:00Z",\n  "priority": "medium"\n}\n```\n\n**Response (`201 Created`):**\n```json\n{\n  "id": "td_abc123",\n  "title": "Buy groceries",\n  "description": "Milk, eggs, bread",\n  "completed": false,\n  "dueDate": "2025-03-15T10:00:00Z",\n  "priority": "medium",\n  "createdAt": "2025-03-10T14:30:00Z",\n  "updatedAt": "2025-03-10T14:30:00Z"\n}\n```\n\nThe `title` field is required — everything else is optional. The server generates `id`, `completed` (defaults to `false`), and timestamps.',
        },
      ],
    },
    {
      role: 'user',
      content: [{ type: 'text', text: 'What about validation errors?' }],
    },
    {
      role: 'assistant',
      content: [
        {
          type: 'text',
          text: 'For validation errors, return `422 Unprocessable Entity` with a structured error body:\n\n```json\n{\n  "error": "validation_error",\n  "message": "Request validation failed",\n  "details": [\n    {\n      "field": "title",\n      "message": "Title is required and must be between 1-200 characters"\n    },\n    {\n      "field": "priority",\n      "message": "Must be one of: low, medium, high"\n    }\n  ]\n}\n```\n\nThis gives clients enough information to display field-level errors in the UI. Keep the `details` array consistent across all endpoints.',
        },
      ],
    },
  ],
}

// ---------------------------------------------------------------------------
// Stories
// ---------------------------------------------------------------------------

/**
 * Basic text conversation replay. Messages stream in character-by-character
 * with a typing effect, just like a real chat session.
 */
export const TextConversation: Story = () => (
  <Replay
    cassette={textOnlyCassette}
    config={{ variant: 'standalone' }}
    typingSpeed={10}
    userMessageDelay={600}
  >
    <Chat />
  </Replay>
)
TextConversation.decorators = [
  (Story) => (
    <div className="m-auto flex h-screen w-full max-w-3xl flex-col px-4">
      <Story />
    </div>
  ),
]

/**
 * Replay with tool calls. The assistant invokes tools with visible
 * arguments and results, demonstrating the tool call rendering pipeline.
 */
export const ToolCalls: Story = () => (
  <Replay
    cassette={toolCallCassette}
    config={{
      variant: 'standalone',
      tools: { expandToolGroupsByDefault: true },
    }}
    typingSpeed={10}
    userMessageDelay={600}
  >
    <Chat />
  </Replay>
)
ToolCalls.decorators = [
  (Story) => (
    <div className="m-auto flex h-screen w-full max-w-3xl flex-col px-4">
      <Story />
    </div>
  ),
]

/**
 * Replay with reasoning (chain-of-thought) content. The assistant's
 * internal reasoning is shown before the final response.
 */
export const Reasoning: Story = () => (
  <Replay
    cassette={reasoningCassette}
    config={{ variant: 'standalone' }}
    typingSpeed={10}
    userMessageDelay={600}
  >
    <Chat />
  </Replay>
)
Reasoning.decorators = [
  (Story) => (
    <div className="m-auto flex h-screen w-full max-w-3xl flex-col px-4">
      <Story />
    </div>
  ),
]

/**
 * Multi-turn conversation with multiple user/assistant exchanges,
 * demonstrating extended conversations with rich markdown formatting.
 */
export const MultiTurn: Story = () => (
  <Replay
    cassette={multiTurnCassette}
    config={{ variant: 'standalone' }}
    typingSpeed={8}
    userMessageDelay={500}
    assistantStartDelay={300}
  >
    <Chat />
  </Replay>
)
MultiTurn.decorators = [
  (Story) => (
    <div className="m-auto flex h-screen w-full max-w-3xl flex-col px-4">
      <Story />
    </div>
  ),
]

/**
 * Replay rendered in the widget (modal) variant, showing how replays
 * work inside a modal container.
 */
export const WidgetVariant: Story = () => (
  <Replay
    cassette={textOnlyCassette}
    config={{
      variant: 'widget',
      modal: { defaultOpen: true },
    }}
    typingSpeed={10}
    userMessageDelay={600}
  >
    <Chat />
  </Replay>
)

/**
 * Fast replay with minimal delays — useful for quick demos or testing.
 */
export const FastReplay: Story = () => (
  <Replay
    cassette={multiTurnCassette}
    config={{ variant: 'standalone' }}
    typingSpeed={2}
    userMessageDelay={200}
    assistantStartDelay={100}
  >
    <Chat />
  </Replay>
)
FastReplay.decorators = [
  (Story) => (
    <div className="m-auto flex h-screen w-full max-w-3xl flex-col px-4">
      <Story />
    </div>
  ),
]
