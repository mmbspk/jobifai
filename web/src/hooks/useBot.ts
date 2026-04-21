import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { botApi } from '../api/bot'
import type { BotState } from '../types'

export function useBot() {
  const qc = useQueryClient()
  const [startError, setStartError] = useState<string | null>(null)

  const { data: status } = useQuery({
    queryKey: ['bot-status'],
    queryFn: botApi.status,
    refetchInterval: (q) => {
      const s = q.state.data?.state as BotState | undefined
      return s === 'running' || s === 'pending_review' ? 5000 : false
    },
    refetchIntervalInBackground: false,
  })

  const start = useMutation({
    mutationFn: (platform: string) => botApi.start(platform),
    onSuccess: () => {
      setStartError(null)
      qc.invalidateQueries({ queryKey: ['bot-status'] })
    },
    onError: (err: Error) => setStartError(err.message),
  })

  const stop = useMutation({
    mutationFn: () => botApi.stop(),
    onSuccess: () => {
      setStartError(null)
      qc.invalidateQueries({ queryKey: ['bot-status'] })
    },
  })

  return { status, start, stop, startError }
}
