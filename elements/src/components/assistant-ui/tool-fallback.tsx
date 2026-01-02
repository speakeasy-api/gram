import { cn } from '@/lib/utils'
import {
  useAssistantState,
  type ToolCallMessagePartComponent,
} from '@assistant-ui/react'
import {
  AlertCircleIcon,
  CheckIcon,
  ChevronDown,
  ChevronUp,
  Loader,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { Button } from '../ui/button'
import { ToolCallResultContent, ToolCallResultSchema } from '@/types/schemas'
import { humanizeToolName } from '@/lib/humanize'
import { useDensity } from '@/hooks/useDensity'
import { BundledLanguage, codeToHtml } from 'shiki'

export const ToolFallback: ToolCallMessagePartComponent = ({
  toolName,
  status,
  result,
  args,
}) => {
  const message = useAssistantState(({ message }) => message)
  const toolParts = message.parts.filter((part) => part.type === 'tool-call')
  const matchingMessagePartIndex = toolParts.findIndex(
    (part) => part.toolName === toolName
  )
  const icon = useMemo(() => {
    if (status.type === 'complete')
      return <CheckIcon className="size-4 text-emerald-500" />
    if (status.type === 'incomplete')
      return <AlertCircleIcon className="size-4 text-rose-500" />
    return <Loader className="text-muted-foreground size-4 animate-spin" />
  }, [status])
  const d = useDensity()
  const [isOpen, setIsOpen] = useState(false)

  const handleOpenChange = (e: React.MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation()
    setIsOpen(!isOpen)
  }
  return (
    <div
      className={cn(
        'aui-tool-fallback-root flex w-full flex-col',
        matchingMessagePartIndex !== -1 && 'border-b',
        matchingMessagePartIndex === toolParts.length - 1 && 'border-b-0'
      )}
    >
      <button
        className={cn(
          'aui-tool-fallback-header flex h-full w-full cursor-pointer items-center',
          d('py-xs'),
          d('px-md'),
          d('gap-md')
        )}
        onClick={handleOpenChange}
      >
        <div>{icon}</div>
        <div className="select-none">{humanizeToolName(toolName)}</div>
        <div className="ml-auto">
          <Button
            variant="ghost"
            size="icon"
            className="cursor-pointer hover:bg-transparent"
            onClick={handleOpenChange}
          >
            {isOpen ? (
              <ChevronUp className="size-4" />
            ) : (
              <ChevronDown className="size-4" />
            )}
          </Button>
        </div>
      </button>
      <div
        className="bg-muted flex overflow-hidden transition-all duration-300 ease-out"
        style={{
          maxHeight: isOpen ? '1000px' : '0',
        }}
      >
        {isOpen && <ToolResultTabs result={result} args={args} />}
      </div>
    </div>
  )
}

type Tab = 'args' | 'result'

function ToolResultTabs({ result, args }: { result: unknown; args: unknown }) {
  const [activeTab, setActiveTab] = useState<Tab>('result')

  const handleTabClick = (e: React.MouseEvent<HTMLButtonElement>, tab: Tab) => {
    e.stopPropagation()
    setActiveTab(tab)
  }

  return (
    <div className="flex w-full flex-col border-t">
      <PillTabs
        tabs={[
          { id: 'result', label: 'Output' },
          { id: 'args', label: 'Arguments' },
        ]}
        activeTab={activeTab}
        onTabClick={handleTabClick}
      />

      <div className="bg-background relative flex w-full flex-col items-start gap-4 border-t">
        <div
          className={cn(
            'w-full transition-opacity',
            activeTab === 'args'
              ? 'opacity-100'
              : 'pointer-events-none absolute opacity-0'
          )}
        >
          <CodeBlock text={JSON.stringify(args, null, 2)} language="json" />
        </div>
        <div
          className={cn(
            'w-full transition-opacity',
            activeTab === 'result'
              ? 'opacity-100'
              : 'pointer-events-none absolute opacity-0'
          )}
        >
          <ToolResultContent result={result} />
        </div>
      </div>
    </div>
  )
}

function PillTabs({
  tabs,
  activeTab,
  onTabClick,
}: {
  tabs: Array<{ id: Tab; label: string }>
  activeTab: Tab
  onTabClick: (
    e: React.MouseEvent<HTMLButtonElement>,
    tab: 'args' | 'result'
  ) => void
}) {
  return (
    <div className="bg-muted flex h-full items-center px-3 py-2">
      {tabs.map((tab) => (
        <button
          key={tab.id}
          onClick={(e) => onTabClick(e, tab.id as 'args' | 'result')}
          className={cn(
            'cursor-pointer rounded-md px-3 py-1 text-sm font-medium transition-all',
            activeTab === tab.id
              ? 'bg-background text-foreground shadow-sm'
              : 'text-muted-foreground hover:text-foreground'
          )}
        >
          {tab.label}
        </button>
      ))}
    </div>
  )
}

function ToolResultContent({
  result,
  className,
}: {
  result: unknown
  className?: string
}) {
  const parsed = ToolCallResultSchema.parse(result)

  return (
    <div className={cn('w-full', className)}>
      {parsed?.content.map((item: ToolCallResultContent, index) => {
        switch (item.type) {
          case 'text': {
            const language = getLanguageFromMimeType(
              item._meta?.['getgram.ai/mime-type'] ?? 'text/plain'
            )
            const text = getFormattedText(item.text, language)
            return <CodeBlock key={item.text} text={text} language={language} />
          }
          case 'image': {
            // image is a base 64 encoded image
            const image = `data:image/png;base64,${item.data}`
            return (
              <div
                key={index}
                className="flex items-center justify-center rounded-lg p-5"
                style={{
                  backgroundImage: `linear-gradient(45deg, #ccc 25%, transparent 25%), 
                                    linear-gradient(135deg, #ccc 25%, transparent 25%),
                                    linear-gradient(45deg, transparent 75%, #ccc 75%),
                                    linear-gradient(135deg, transparent 75%, #ccc 75%)`,
                  backgroundSize: '25px 25px',
                  backgroundPosition:
                    '0 0, 12.5px 0, 12.5px -12.5px, 0px 12.5px',
                }}
              >
                <img
                  src={image}
                  className="max-h-[300px] max-w-full object-contain"
                />
              </div>
            )
          }
          default:
            return (
              <pre
                key={index}
                className="aui-tool-fallback-result-content whitespace-pre-wrap"
              >
                {JSON.stringify(item, null, 2)}
              </pre>
            )
        }
      })}
    </div>
  )
}

function getLanguageFromMimeType(
  mimeType: string
): BundledLanguage | undefined {
  let language: BundledLanguage | undefined
  switch (mimeType) {
    case 'text/markdown':
      language = 'markdown'
      break
    case 'text/html':
      language = 'html'
      break
    case 'text/css':
      language = 'css'
      break
    case 'application/json':
      language = 'json'
      break
    case 'text/javascript':
      language = 'javascript'
      break
    case 'text/typescript':
      language = 'typescript'
      break
    case 'text/python':
      language = 'python'
      break
    default:
      return undefined
  }
  return language
}

function CodeBlock({
  text,
  language,
  className,
}: {
  text: string
  className?: string
  language: BundledLanguage | undefined
}) {
  const [highlightedCode, setHighlightedCode] = useState<string | null>(null)
  useEffect(() => {
    if (!language) return
    codeToHtml(text, {
      lang: language,
      theme: 'github-dark-default',

      rootStyle: 'background-color: transparent;',
      transformers: [
        {
          pre(node) {
            node.properties.class =
              'w-full py-3 px-5 max-h-[300px] overflow-y-auto whitespace-pre-wrap text-left'
          },
        },
      ],
    }).then(setHighlightedCode)
  }, [text])

  if (!highlightedCode)
    return (
      <pre
        className={cn(
          'aui-tool-fallback-result-content text-background flex w-full items-start bg-slate-800/90 px-5 py-3 whitespace-normal',
          className
        )}
      >
        {text}
      </pre>
    )
  return (
    <div
      className={cn(
        'aui-tool-fallback-result-content w-full bg-slate-800/90',
        className
      )}
      dangerouslySetInnerHTML={{ __html: highlightedCode }}
    />
  )
}

function getFormattedText(text: string, language: BundledLanguage | undefined) {
  if (!language) return text
  switch (language) {
    case 'json':
      return JSON.stringify(JSON.parse(text), null, 2)
    default:
      return text
  }
}
