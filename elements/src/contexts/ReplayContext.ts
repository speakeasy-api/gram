import { createContext, useContext } from 'react'

export const ReplayContext = createContext<{ isReplay: boolean } | null>(null)

export function useReplayContext() {
  return useContext(ReplayContext)
}
