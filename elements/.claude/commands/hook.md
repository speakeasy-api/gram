---
description: Create a custom React hook
---

# Create Hook

Create a custom React hook following elements library patterns.

## Process

1. **Define the hook's purpose**:
   - What state does it manage?
   - What side effects does it handle?
   - What does it return?

2. **Create the hook file**:

   ```tsx
   // src/hooks/useMyHook.ts
   import { useState, useCallback, useMemo, useEffect } from 'react'

   // Options interface
   export interface UseMyHookOptions {
     initialValue?: string
     onSuccess?: (result: string) => void
     onError?: (error: Error) => void
   }

   // Return type interface
   export interface UseMyHookReturn {
     // State
     value: string
     isLoading: boolean
     error: Error | null

     // Actions
     setValue: (value: string) => void
     reset: () => void
     submit: () => Promise<void>
   }

   /**
    * Hook description - what it does and when to use it.
    *
    * @example
    * ```tsx
    * const { value, setValue, submit } = useMyHook({
    *   initialValue: 'default',
    *   onSuccess: (result) => console.log(result),
    * })
    * ```
    */
   export function useMyHook(
     options: UseMyHookOptions = {}
   ): UseMyHookReturn {
     const {
       initialValue = '',
       onSuccess,
       onError,
     } = options

     // State
     const [value, setValue] = useState(initialValue)
     const [isLoading, setIsLoading] = useState(false)
     const [error, setError] = useState<Error | null>(null)

     // Actions (memoized)
     const reset = useCallback(() => {
       setValue(initialValue)
       setError(null)
     }, [initialValue])

     const submit = useCallback(async () => {
       setIsLoading(true)
       setError(null)

       try {
         // Async operation
         const result = await someAsyncOperation(value)
         onSuccess?.(result)
       } catch (err) {
         const error = err instanceof Error ? err : new Error(String(err))
         setError(error)
         onError?.(error)
       } finally {
         setIsLoading(false)
       }
     }, [value, onSuccess, onError])

     // Return memoized object
     return useMemo(() => ({
       value,
       isLoading,
       error,
       setValue,
       reset,
       submit,
     }), [value, isLoading, error, reset, submit])
   }
   ```

3. **Create tests**:

   ```tsx
   // src/hooks/useMyHook.test.ts
   import { renderHook, act, waitFor } from '@testing-library/react'
   import { describe, it, expect, vi } from 'vitest'
   import { useMyHook } from './useMyHook'

   describe('useMyHook', () => {
     it('initializes with default value', () => {
       const { result } = renderHook(() => useMyHook())
       expect(result.current.value).toBe('')
       expect(result.current.isLoading).toBe(false)
       expect(result.current.error).toBeNull()
     })

     it('accepts initial value option', () => {
       const { result } = renderHook(() =>
         useMyHook({ initialValue: 'test' })
       )
       expect(result.current.value).toBe('test')
     })

     it('updates value via setValue', () => {
       const { result } = renderHook(() => useMyHook())

       act(() => {
         result.current.setValue('new value')
       })

       expect(result.current.value).toBe('new value')
     })

     it('resets to initial value', () => {
       const { result } = renderHook(() =>
         useMyHook({ initialValue: 'initial' })
       )

       act(() => {
         result.current.setValue('changed')
         result.current.reset()
       })

       expect(result.current.value).toBe('initial')
     })

     it('calls onSuccess callback', async () => {
       const onSuccess = vi.fn()
       const { result } = renderHook(() => useMyHook({ onSuccess }))

       await act(async () => {
         await result.current.submit()
       })

       expect(onSuccess).toHaveBeenCalled()
     })
   })
   ```

4. **Export from hooks index** (if exists):
   ```tsx
   // src/hooks/index.ts
   export * from './useMyHook'
   ```

5. **Run tests**:
   ```bash
   pnpm test -- useMyHook
   ```

## Hook Checklist

- [ ] Has TypeScript interfaces for options and return
- [ ] Uses `useCallback` for action functions
- [ ] Uses `useMemo` for return object (prevents re-renders)
- [ ] Has JSDoc with @example
- [ ] Handles errors properly
- [ ] Has unit tests
- [ ] Uses `@/` alias imports

## Arguments
- `$ARGUMENTS`: Hook name and purpose (e.g., "useDebounce for debouncing input values")
