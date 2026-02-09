'use client'

import { GenerativeUI } from '@/components/ui/generative-ui'
import { cn } from '@/lib/utils'
import { SyntaxHighlighterProps } from '@assistant-ui/react-markdown'
import { FC, useEffect, useMemo, useState } from 'react'
import { MacOSWindowFrame } from '../components/MacOSWindowFrame'

const loadingMessages = [
  // Crafting & Creating
  'Arranging pixels with care...',
  'Brewing something beautiful...',
  'Crafting your masterpiece...',
  'Painting with data...',
  'Weaving digital magic...',
  'Assembling the good stuff...',
  'Polishing the details...',
  'Putting the finishing touches...',
  // Cooking & Food
  'Simmering the results...',
  'Letting the data marinate...',
  'Adding a pinch of style...',
  'Fresh out of the oven soon...',
  'Whisking up your view...',
  // Nature & Growth
  'Growing your garden of data...',
  'Watching the seeds sprout...',
  'Letting things bloom...',
  'Nature is taking its course...',
  // Space & Magic
  'Consulting the stars...',
  'Channeling cosmic energy...',
  'Summoning the results...',
  'Waving the magic wand...',
  'Sprinkling some stardust...',
  'Aligning the planets...',
  // Building & Engineering
  'Tightening the bolts...',
  'Connecting the dots...',
  'Stacking the blocks...',
  'Laying the foundation...',
  'Raising the scaffolding...',
  // Playful & Cute
  'Herding the pixels...',
  'Teaching data to dance...',
  'Convincing bits to cooperate...',
  'Giving electrons a pep talk...',
  'Wrangling the numbers...',
  'Coaxing the results out...',
  'Almost there, pinky promise...',
  'Good things take a moment...',
  'Worth the wait...',
  'Patience, grasshopper...',
  'Hold tight...',
  'Doing the thing...',
  // Abstract & Poetic
  'Folding space and time...',
  'Untangling the threads...',
  'Finding the signal...',
  'Distilling the essence...',
  'Turning chaos into order...',
  'Making sense of it all...',
  // Confident & Reassuring
  'This is going to be good...',
  "You're gonna love this...",
  'Something nice is coming...',
  'Just a heartbeat away...',
]

function getRandomStartIndex() {
  return Math.floor(Math.random() * loadingMessages.length)
}

const CyclingLoadingMessage: FC = () => {
  const [index, setIndex] = useState(getRandomStartIndex)
  const [isVisible, setIsVisible] = useState(true)

  useEffect(() => {
    let timeoutId: ReturnType<typeof setTimeout> | undefined
    const interval = setInterval(() => {
      // Fade out
      setIsVisible(false)

      // After fade out, change message and fade in
      timeoutId = setTimeout(() => {
        setIndex((prev) => (prev + 1) % loadingMessages.length)
        setIsVisible(true)
      }, 200)
    }, 2000)

    return () => {
      clearInterval(interval)
      if (timeoutId) clearTimeout(timeoutId)
    }
  }, [])

  return (
    <span
      className={cn(
        'shimmer text-muted-foreground text-sm transition-opacity duration-200',
        isVisible ? 'opacity-100' : 'opacity-0'
      )}
    >
      {loadingMessages[index]}
    </span>
  )
}

export const GenerativeUIRenderer: FC<SyntaxHighlighterProps> = ({ code }) => {
  // Parse JSON - returns null if invalid (still streaming)
  const content = useMemo(() => {
    const trimmedCode = code.trim()
    if (!trimmedCode) return null

    try {
      const parsed = JSON.parse(trimmedCode)
      // Validate it has a type field (basic json-render structure)
      if (!parsed || typeof parsed !== 'object' || !('type' in parsed)) {
        return null
      }
      return parsed
    } catch {
      // JSON is incomplete (still streaming) - return null to show loading state
      return null
    }
  }, [code])

  // Show loading state while JSON is incomplete/streaming
  if (!content) {
    return (
      <MacOSWindowFrame>
        <div className="bg-background flex min-h-[400px] items-center justify-center">
          <CyclingLoadingMessage />
        </div>
      </MacOSWindowFrame>
    )
  }

  // Render with macOS-style window frame
  return (
    <MacOSWindowFrame>
      <div className="p-4">
        <GenerativeUI content={content} />
      </div>
    </MacOSWindowFrame>
  )
}
