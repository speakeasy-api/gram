'use client'

import '@assistant-ui/react-markdown/styles/dot.css'

import {
  type CodeHeaderProps,
  MarkdownTextPrimitive,
  unstable_memoizeMarkdownComponents as memoizeMarkdownComponents,
  useIsMarkdownCodeBlock,
} from '@assistant-ui/react-markdown'
import { CheckIcon, CopyIcon } from 'lucide-react'
import { type FC, memo, useState } from 'react'
import remarkGfm from 'remark-gfm'

import { TooltipIconButton } from '@/components/assistant-ui/tooltip-icon-button'
import { cn } from '@/lib/utils'
import { useElements } from '@/hooks/useElements'
import { useComponentsByLanguage } from '@/hooks/usePluginComponents'
import { useAssistantState } from '@assistant-ui/react'

const MarkdownTextImpl = () => {
  const { plugins } = useElements()
  const componentsByLanguage = useComponentsByLanguage(plugins)

  return (
    <MarkdownTextPrimitive
      remarkPlugins={[remarkGfm]}
      className="aui-md"
      components={defaultComponents}
      componentsByLanguage={componentsByLanguage}
    />
  )
}

export const MarkdownText = memo(MarkdownTextImpl)

const CodeHeader: FC<CodeHeaderProps> = ({ language, code }) => {
  const message = useAssistantState(({ message }) => message)
  const messageIsComplete = message.status?.type === 'complete'
  const { isCopied, copyToClipboard } = useCopyToClipboard()
  const onCopy = () => {
    if (!code || isCopied) return
    copyToClipboard(code)
  }

  if (!messageIsComplete) {
    return null
  }
  return (
    <div className="aui-code-header-root gramel:bg-muted-foreground/15 gramel:text-foreground gramel:dark:bg-muted-foreground/20 gramel:mt-4 gramel:flex gramel:items-center gramel:justify-between gramel:gap-4 gramel:rounded-t-lg gramel:px-4 gramel:py-2 gramel:text-sm gramel:font-semibold">
      <span className="aui-code-header-language gramel:lowercase gramel:[&>span]:text-xs">
        {language}
      </span>
      <TooltipIconButton tooltip="Copy" onClick={onCopy}>
        {!isCopied && <CopyIcon />}
        {isCopied && <CheckIcon />}
      </TooltipIconButton>
    </div>
  )
}

const useCopyToClipboard = ({
  copiedDuration = 3000,
}: {
  copiedDuration?: number
} = {}) => {
  const [isCopied, setIsCopied] = useState<boolean>(false)

  const copyToClipboard = (value: string) => {
    if (!value) return

    navigator.clipboard.writeText(value).then(() => {
      setIsCopied(true)
      setTimeout(() => setIsCopied(false), copiedDuration)
    })
  }

  return { isCopied, copyToClipboard }
}

const defaultComponents = memoizeMarkdownComponents({
  h1: ({ className, ...props }) => (
    <h1
      className={cn('aui-md-h1 gramel:mb-8 gramel:scroll-m-20 gramel:text-4xl gramel:font-extrabold gramel:tracking-tight gramel:last:mb-0',
        className
      )}
      {...props}
    />
  ),
  h2: ({ className, ...props }) => (
    <h2
      className={cn('aui-md-h2 gramel:mt-8 gramel:mb-4 gramel:scroll-m-20 gramel:text-3xl gramel:font-semibold gramel:tracking-tight gramel:first:mt-0 gramel:last:mb-0',
        className
      )}
      {...props}
    />
  ),
  h3: ({ className, ...props }) => (
    <h3
      className={cn('aui-md-h3 gramel:mt-6 gramel:mb-4 gramel:scroll-m-20 gramel:text-2xl gramel:font-semibold gramel:tracking-tight gramel:first:mt-0 gramel:last:mb-0',
        className
      )}
      {...props}
    />
  ),
  h4: ({ className, ...props }) => (
    <h4
      className={cn('aui-md-h4 gramel:mt-6 gramel:mb-4 gramel:scroll-m-20 gramel:text-xl gramel:font-semibold gramel:tracking-tight gramel:first:mt-0 gramel:last:mb-0',
        className
      )}
      {...props}
    />
  ),
  h5: ({ className, ...props }) => (
    <h5
      className={cn('aui-md-h5 gramel:my-4 gramel:text-lg gramel:font-semibold gramel:first:mt-0 gramel:last:mb-0',
        className
      )}
      {...props}
    />
  ),
  h6: ({ className, ...props }) => (
    <h6
      className={cn('aui-md-h6 gramel:my-4 gramel:font-semibold gramel:first:mt-0 gramel:last:mb-0',
        className
      )}
      {...props}
    />
  ),
  p: ({ className, ...props }) => (
    <p
      className={cn('aui-md-p gramel:mt-5 gramel:mb-5 gramel:leading-7 gramel:first:mt-0 gramel:last:mb-0',
        className
      )}
      {...props}
    />
  ),
  a: ({ className, ...props }) => (
    <a
      className={cn('aui-md-a gramel:text-primary gramel:font-medium gramel:underline gramel:underline-offset-4',
        className
      )}
      {...props}
    />
  ),
  blockquote: ({ className, ...props }) => (
    <blockquote
      className={cn('aui-md-blockquote gramel:border-l-2 gramel:pl-6 gramel:italic', className)}
      {...props}
    />
  ),
  ul: ({ className, ...props }) => (
    <ul
      className={cn('aui-md-ul gramel:my-5 gramel:ml-6 gramel:list-disc gramel:[&>li]:mt-2', className)}
      {...props}
    />
  ),
  ol: ({ className, ...props }) => (
    <ol
      className={cn('aui-md-ol gramel:my-5 gramel:ml-6 gramel:list-decimal gramel:[&>li]:mt-2', className)}
      {...props}
    />
  ),
  hr: ({ className, ...props }) => (
    <hr className={cn('aui-md-hr gramel:my-5 gramel:border-b', className)} {...props} />
  ),
  table: ({ className, ...props }) => (
    <table
      className={cn('aui-md-table gramel:my-5 gramel:w-full gramel:border-separate gramel:border-spacing-0 gramel:overflow-y-auto',
        className
      )}
      {...props}
    />
  ),
  th: ({ className, ...props }) => (
    <th
      className={cn('aui-md-th gramel:bg-muted gramel:px-4 gramel:py-2 gramel:text-left gramel:font-bold gramel:first:rounded-tl-lg gramel:last:rounded-tr-lg gramel:[[align=center]]:text-center gramel:[[align=right]]:text-right',
        className
      )}
      {...props}
    />
  ),
  td: ({ className, ...props }) => (
    <td
      className={cn('aui-md-td gramel:border-b gramel:border-l gramel:px-4 gramel:py-2 gramel:text-left gramel:last:border-r gramel:[[align=center]]:text-center gramel:[[align=right]]:text-right',
        className
      )}
      {...props}
    />
  ),
  tr: ({ className, ...props }) => (
    <tr
      className={cn('aui-md-tr gramel:m-0 gramel:border-b gramel:p-0 gramel:first:border-t gramel:[&:last-child>td:first-child]:rounded-bl-lg gramel:[&:last-child>td:last-child]:rounded-br-lg',
        className
      )}
      {...props}
    />
  ),
  sup: ({ className, ...props }) => (
    <sup
      className={cn('aui-md-sup gramel:[&>a]:text-xs gramel:[&>a]:no-underline', className)}
      {...props}
    />
  ),
  pre: ({ className, ...props }) => (
    <pre
      className={cn('aui-md-pre gramel:text-foreground gramel:bg-muted gramel:overflow-x-auto gramel:rounded-t-none! gramel:rounded-b-lg gramel:border gramel:border-t-0 gramel:p-4',
        className
      )}
      {...props}
    />
  ),
  code: function Code({ className, ...props }) {
    const isCodeBlock = useIsMarkdownCodeBlock()
    return (
      <code
        className={cn(
          !isCodeBlock &&
            'aui-md-inline-code gramel:bg-muted gramel:rounded gramel:border gramel:font-semibold',
          className
        )}
        {...props}
      />
    )
  },
  CodeHeader,
})
