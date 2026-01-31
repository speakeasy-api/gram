'use client'

import * as React from 'react'
import { useCallback, useRef, useState } from 'react'
import { ArrowLeftIcon, Loader2Icon, PlayIcon } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { CommandBarToolMeta } from '@/types'

interface SchemaProperty {
  type?: string
  description?: string
  enum?: unknown[]
  default?: unknown
  properties?: Record<string, SchemaProperty>
  required?: string[]
}

/** A leaf field after flattening nested objects. */
interface FlatField {
  /** Dot-notation path, e.g. "queryParameters.cardNumber" */
  path: string
  schema: SchemaProperty
  required: boolean
  /** Parent group label for visual grouping, e.g. "Query Parameters" */
  groupLabel?: string
}

interface CommandBarToolFormProps {
  toolMeta: CommandBarToolMeta
  onSubmit: (args: Record<string, unknown>) => void
  onBack: () => void
  isExecuting: boolean
  className?: string
}

/**
 * Unwraps the AI SDK `StandardSchemaV1` wrapper if present and returns
 * the raw JSON Schema object.
 */
function unwrapSchema(parameters: Record<string, unknown>): Record<string, unknown> {
  let schema = parameters
  if ('jsonSchema' in schema && typeof schema.jsonSchema === 'object' && schema.jsonSchema !== null) {
    schema = schema.jsonSchema as Record<string, unknown>
  }
  return schema
}

/**
 * Recursively flattens nested object properties into leaf fields.
 * Objects with their own `properties` are expanded; everything else
 * becomes a renderable field.
 */
function flattenSchema(
  properties: Record<string, SchemaProperty>,
  requiredKeys: string[],
  prefix = '',
  groupLabel?: string
): FlatField[] {
  const fields: FlatField[] = []
  for (const [key, prop] of Object.entries(properties)) {
    const path = prefix ? `${prefix}.${key}` : key
    const isRequired = requiredKeys.includes(key)

    if (
      prop.type === 'object' &&
      prop.properties &&
      Object.keys(prop.properties).length > 0 &&
      !prop.enum
    ) {
      // Recurse into nested object
      const innerRequired = prop.required ?? []
      fields.push(
        ...flattenSchema(
          prop.properties,
          innerRequired,
          path,
          humanizeFieldName(key)
        )
      )
    } else {
      fields.push({ path, schema: prop, required: isRequired, groupLabel })
    }
  }
  return fields
}

/**
 * Builds the flat field list from a tool's parameter schema.
 */
function getSchemaFields(parameters: Record<string, unknown>): FlatField[] {
  const schema = unwrapSchema(parameters)
  const props = (schema.properties ?? {}) as Record<string, SchemaProperty>
  const req = (schema.required ?? []) as string[]
  return flattenSchema(props, req)
}

/**
 * Reconstructs a nested object from dot-notation keyed flat values.
 * e.g. { "queryParameters.cardNumber": "123" } → { queryParameters: { cardNumber: "123" } }
 */
function unflattenValues(flat: Record<string, unknown>): Record<string, unknown> {
  const result: Record<string, unknown> = {}
  for (const [path, value] of Object.entries(flat)) {
    const parts = path.split('.')
    let current = result
    for (let i = 0; i < parts.length - 1; i++) {
      if (!(parts[i] in current)) {
        current[parts[i]] = {}
      }
      current = current[parts[i]] as Record<string, unknown>
    }
    current[parts[parts.length - 1]] = value
  }
  return result
}

function humanizeFieldName(name: string): string {
  return name
    .replace(/_/g, ' ')
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .replace(/\b\w/g, (c) => c.toUpperCase())
}

export function CommandBarToolForm({
  toolMeta,
  onSubmit,
  onBack,
  isExecuting,
  className,
}: CommandBarToolFormProps) {
  const fields = getSchemaFields(toolMeta.parameters)
  const hasFields = fields.length > 0

  const [values, setValues] = useState<Record<string, unknown>>(() => {
    const initial: Record<string, unknown> = {}
    for (const { path, schema } of fields) {
      if (schema.default !== undefined) {
        initial[path] = schema.default
      } else if (schema.type === 'boolean') {
        initial[path] = false
      } else {
        initial[path] = ''
      }
    }
    return initial
  })

  const firstInputRef = useRef<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>(null)

  React.useEffect(() => {
    // Focus the first input after mount
    const t = setTimeout(() => firstInputRef.current?.focus(), 50)
    return () => clearTimeout(t)
  }, [])

  const setValue = useCallback((key: string, value: unknown) => {
    setValues((prev) => ({ ...prev, [key]: value }))
  }, [])

  const handleSubmit = useCallback(
    (e: React.FormEvent) => {
      e.preventDefault()
      e.stopPropagation()

      // Coerce flat values to appropriate types based on schema
      const coerced: Record<string, unknown> = {}
      for (const { path, schema, required: isReq } of fields) {
        const raw = values[path]
        if (raw === '' && !isReq) continue
        if (schema.type === 'number' || schema.type === 'integer') {
          coerced[path] = Number(raw)
        } else if (schema.type === 'boolean') {
          coerced[path] = Boolean(raw)
        } else if (schema.type === 'object' || schema.type === 'array') {
          try {
            coerced[path] = JSON.parse(String(raw))
          } catch {
            coerced[path] = raw
          }
        } else {
          coerced[path] = raw
        }
      }
      // Reconstruct nested structure from dot-notation keys
      onSubmit(unflattenValues(coerced))
    },
    [values, fields, onSubmit]
  )

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      // Prevent cmdk from capturing these keys
      e.stopPropagation()
      if (e.key === 'Escape') {
        e.preventDefault()
        onBack()
      }
    },
    [onBack]
  )

  return (
    <div
      data-slot="command-bar-tool-form"
      className={cn('flex flex-col', className)}
      onKeyDown={handleKeyDown}
    >
      {/* Header */}
      <div className="flex items-center gap-2 border-b px-3 py-2.5">
        <button
          type="button"
          onClick={onBack}
          className="text-muted-foreground hover:text-foreground -ml-0.5 rounded p-0.5 transition-colors"
          aria-label="Back to command list"
        >
          <ArrowLeftIcon className="size-4" />
        </button>
        <div className="min-w-0 flex-1">
          <div className="text-foreground text-sm font-medium">
            {humanizeFieldName(toolMeta.toolName)}
          </div>
          {toolMeta.description && (
            <div className="text-muted-foreground truncate text-xs">
              {toolMeta.description}
            </div>
          )}
        </div>
      </div>

      {/* Form */}
      <form
        onSubmit={handleSubmit}
        className="max-h-[300px] overflow-y-auto px-3 py-2"
      >
        {hasFields ? (
          <div className="space-y-3">
            {fields.map((field, i) => {
              // Show group label when the group changes
              const prevGroup = i > 0 ? fields[i - 1].groupLabel : undefined
              const showGroup = field.groupLabel && field.groupLabel !== prevGroup
              return (
                <React.Fragment key={field.path}>
                  {showGroup && (
                    <div className="text-muted-foreground border-b pb-1 text-[10px] font-medium uppercase tracking-wider">
                      {field.groupLabel}
                    </div>
                  )}
                  <FieldRenderer
                    name={field.path}
                    schema={field.schema}
                    required={field.required}
                    value={values[field.path]}
                    onChange={(v) => setValue(field.path, v)}
                    ref={i === 0 ? firstInputRef : undefined}
                  />
                </React.Fragment>
              )
            })}
          </div>
        ) : (
          <p className="text-muted-foreground py-2 text-sm">
            This tool has no parameters.
          </p>
        )}

        <button
          type="submit"
          disabled={isExecuting}
          className={cn(
            'mt-3 flex w-full items-center justify-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors',
            'bg-primary text-primary-foreground hover:bg-primary/90',
            'disabled:pointer-events-none disabled:opacity-50'
          )}
        >
          {isExecuting ? (
            <>
              <Loader2Icon className="size-3.5 animate-spin" />
              Running...
            </>
          ) : (
            <>
              <PlayIcon className="size-3.5" />
              Execute
            </>
          )}
        </button>
      </form>
    </div>
  )
}

/**
 * Renders a single form field based on JSON Schema property type.
 */
const FieldRenderer = React.forwardRef<
  HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement,
  {
    name: string
    schema: SchemaProperty
    required: boolean
    value: unknown
    onChange: (value: unknown) => void
  }
>(function FieldRenderer({ name, schema, required, value, onChange }, ref) {
  // Use the last segment of the dot path for display
  const leafName = name.includes('.') ? name.split('.').pop()! : name
  const label = humanizeFieldName(leafName)
  const id = `tool-field-${name}`

  const inputClasses = cn(
    'bg-muted border-border text-foreground placeholder:text-muted-foreground w-full rounded-md border px-2.5 py-1.5 text-sm',
    'focus:ring-ring focus:border-ring focus:outline-none focus:ring-1'
  )

  // Enum → select
  if (schema.enum && Array.isArray(schema.enum)) {
    return (
      <FieldWrapper id={id} label={label} description={schema.description} required={required}>
        <select
          ref={ref as React.Ref<HTMLSelectElement>}
          id={id}
          value={String(value ?? '')}
          onChange={(e) => onChange(e.target.value)}
          className={inputClasses}
        >
          {!required && <option value="">Select...</option>}
          {schema.enum.map((v) => (
            <option key={String(v)} value={String(v)}>
              {String(v)}
            </option>
          ))}
        </select>
      </FieldWrapper>
    )
  }

  // Boolean → checkbox
  if (schema.type === 'boolean') {
    return (
      <div className="flex items-center gap-2">
        <input
          ref={ref as React.Ref<HTMLInputElement>}
          type="checkbox"
          id={id}
          checked={Boolean(value)}
          onChange={(e) => onChange(e.target.checked)}
          className="border-border text-primary rounded"
        />
        <label htmlFor={id} className="text-foreground text-sm">
          {label}
        </label>
        {schema.description && (
          <span className="text-muted-foreground text-xs">{schema.description}</span>
        )}
      </div>
    )
  }

  // Number / integer
  if (schema.type === 'number' || schema.type === 'integer') {
    return (
      <FieldWrapper id={id} label={label} description={schema.description} required={required}>
        <input
          ref={ref as React.Ref<HTMLInputElement>}
          type="number"
          id={id}
          value={value === '' ? '' : String(value ?? '')}
          onChange={(e) => onChange(e.target.value)}
          step={schema.type === 'integer' ? '1' : 'any'}
          required={required}
          className={inputClasses}
          placeholder={`Enter ${label.toLowerCase()}...`}
        />
      </FieldWrapper>
    )
  }

  // Object / array → JSON textarea
  if (schema.type === 'object' || schema.type === 'array') {
    return (
      <FieldWrapper id={id} label={label} description={schema.description} required={required}>
        <textarea
          ref={ref as React.Ref<HTMLTextAreaElement>}
          id={id}
          value={String(value ?? '')}
          onChange={(e) => onChange(e.target.value)}
          required={required}
          rows={3}
          className={cn(inputClasses, 'font-mono text-xs')}
          placeholder="Enter JSON..."
        />
      </FieldWrapper>
    )
  }

  // Default: string text input
  return (
    <FieldWrapper id={id} label={label} description={schema.description} required={required}>
      <input
        ref={ref as React.Ref<HTMLInputElement>}
        type="text"
        id={id}
        value={String(value ?? '')}
        onChange={(e) => onChange(e.target.value)}
        required={required}
        className={inputClasses}
        placeholder={`Enter ${label.toLowerCase()}...`}
      />
    </FieldWrapper>
  )
})

function FieldWrapper({
  id,
  label,
  description,
  required,
  children,
}: {
  id: string
  label: string
  description?: string
  required: boolean
  children: React.ReactNode
}) {
  return (
    <div className="space-y-1">
      <label htmlFor={id} className="text-foreground flex items-baseline gap-1 text-xs font-medium">
        {label}
        {required && <span className="text-destructive">*</span>}
      </label>
      {children}
      {description && (
        <p className="text-muted-foreground text-[11px] leading-tight">{description}</p>
      )}
    </div>
  )
}
