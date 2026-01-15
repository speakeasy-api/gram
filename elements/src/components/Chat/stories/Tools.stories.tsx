import React from 'react'
import { Chat } from '..'
import type { Meta, StoryFn } from '@storybook/react-vite'
import { ToolCallMessagePartProps } from '@assistant-ui/react'

const meta: Meta<typeof Chat> = {
  title: 'Chat/Tools',
  component: Chat,
  parameters: {
    layout: 'fullscreen',
  },
  decorators: [
    (Story) => (
      <div className="gramel:m-auto gramel:flex gramel:h-screen gramel:w-full gramel:max-w-3xl gramel:flex-col">
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof Chat>

export default meta

type Story = StoryFn<typeof Chat>

const CardPinRevealComponent = ({
  result,
  argsText,
}: ToolCallMessagePartProps) => {
  const [isFlipped, setIsFlipped] = React.useState(false)

  // Parse the result to get the pin
  let pin = '****'
  try {
    if (result) {
      const parsed = typeof result === 'string' ? JSON.parse(result) : result
      if (parsed?.content?.[0]?.text) {
        const content = JSON.parse(parsed.content[0].text)
        pin = content.pin || '****'
      } else if (parsed?.pin) {
        pin = parsed.pin
      }
    }
  } catch {
    // Fallback to default
  }

  const args = JSON.parse(argsText || '{}')
  const cardNumber = args?.queryParameters?.cardNumber || '4532 •••• •••• 1234'
  const cardHolder = 'JOHN DOE'
  const expiry = '12/25'
  const cvv = '123'

  if (!cardNumber) {
    return null
  }

  return (
    <div className="gramel:my-4 gramel:perspective-[1000px]">
      <div
        className={`gramel:relative gramel:h-48 gramel:w-80 gramel:cursor-pointer gramel:transition-transform gramel:duration-700 gramel:[transform-style:preserve-3d] ${
          isFlipped ? 'transform-[rotateY(180deg)]' : ''
        }`}
        onClick={() => setIsFlipped(!isFlipped)}
      >
        {/* Front of card */}
        <div className="gramel:absolute gramel:inset-0 gramel:backface-hidden">
          <div className="gramel:relative gramel:h-full gramel:w-full gramel:overflow-hidden gramel:rounded-xl gramel:bg-gradient-to-br gramel:from-indigo-600 gramel:via-purple-600 gramel:to-pink-500 gramel:p-6 gramel:text-white gramel:shadow-2xl">
            {/* Card pattern overlay */}
            <div className="gramel:absolute gramel:inset-0 gramel:opacity-10">
              <div className="gramel:absolute gramel:-top-10 gramel:-right-10 gramel:h-40 gramel:w-40 gramel:rounded-full gramel:bg-white"></div>
              <div className="gramel:absolute gramel:-bottom-10 gramel:-left-10 gramel:h-32 gramel:w-32 gramel:rounded-full gramel:bg-white"></div>
            </div>

            {/* Card content */}
            <div className="gramel:relative gramel:z-10 gramel:flex gramel:h-full gramel:flex-col gramel:justify-between">
              <div className="gramel:flex gramel:items-center gramel:justify-between">
                <div className="gramel:text-2xl gramel:font-bold">VISA</div>
                <div className="gramel:h-8 gramel:w-12 gramel:rounded gramel:bg-white/20"></div>
              </div>

              <div className="gramel:space-y-2">
                <div className="gramel:font-mono gramel:text-2xl gramel:tracking-wider">
                  {cardNumber}
                </div>
                <div className="gramel:flex gramel:items-center gramel:justify-between gramel:text-sm">
                  <div>
                    <div className="gramel:text-xs gramel:opacity-70">CARDHOLDER</div>
                    <div className="gramel:font-semibold">{cardHolder}</div>
                  </div>
                  <div>
                    <div className="gramel:text-xs gramel:opacity-70">EXPIRES</div>
                    <div className="gramel:font-semibold">{expiry}</div>
                  </div>
                </div>
              </div>
            </div>

            {/* Click hint */}
            <div className="gramel:absolute gramel:right-2 gramel:bottom-2 gramel:text-xs gramel:opacity-50">
              Click to flip
            </div>
          </div>
        </div>

        {/* Back of card */}
        <div className="gramel:absolute gramel:inset-0 gramel:transform-[rotateY(180deg)] gramel:backface-hidden">
          <div className="gramel:relative gramel:h-full gramel:w-full gramel:overflow-hidden gramel:rounded-xl gramel:bg-gradient-to-br gramel:from-slate-800 gramel:via-slate-700 gramel:to-slate-900 gramel:p-6 gramel:text-white gramel:shadow-2xl">
            {/* Magnetic strip */}
            <div className="gramel:absolute gramel:top-8 gramel:right-0 gramel:left-0 gramel:h-12 gramel:bg-black"></div>

            {/* Card content */}
            <div className="gramel:relative gramel:z-10 gramel:flex gramel:h-full gramel:flex-col gramel:justify-between">
              <div className="gramel:mt-16 gramel:space-y-4">
                <div className="gramel:flex gramel:items-center gramel:gap-2">
                  <div className="gramel:h-8 gramel:flex-1 gramel:rounded gramel:bg-white/10 gramel:px-3 gramel:py-2 gramel:text-right gramel:font-mono gramel:text-sm">
                    {cvv}
                  </div>
                  <div className="gramel:text-xs gramel:opacity-70">CVV</div>
                </div>

                {/* PIN Display */}
                <div className="gramel:mt-6 gramel:space-y-2">
                  <div className="gramel:text-xs gramel:opacity-70">PIN</div>
                  <div className="gramel:flex gramel:items-center gramel:gap-3">
                    <div className="gramel:flex gramel:h-16 gramel:w-16 gramel:items-center gramel:justify-center gramel:rounded-lg gramel:bg-gradient-to-br gramel:from-yellow-400 gramel:to-orange-500 gramel:shadow-lg">
                      <span className="gramel:text-2xl gramel:font-bold gramel:text-white">
                        {pin}
                      </span>
                    </div>
                    <div className="gramel:text-xs gramel:opacity-60">
                      Keep this PIN secure
                    </div>
                  </div>
                </div>
              </div>

              <div className="gramel:flex gramel:items-center gramel:justify-between gramel:text-xs gramel:opacity-50">
                <div>VISA</div>
                <div>{cardNumber}</div>
              </div>
            </div>

            {/* Click hint */}
            <div className="gramel:absolute gramel:bottom-2 gramel:left-2 gramel:text-xs gramel:opacity-50">
              Click to flip back
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

export const CustomToolComponent: Story = () => <Chat />
CustomToolComponent.parameters = {
  elements: {
    config: {
      variant: 'standalone',
      welcome: {
        suggestions: [
          {
            title: 'Get card details',
            label: 'for your card',
            action: 'Get card details for your card number 4532 •••• •••• 1234',
          },
        ],
      },
      tools: {
        components: {
          kitchen_sink_get_get_card_details: CardPinRevealComponent,
        },
      },
    },
  },
}
