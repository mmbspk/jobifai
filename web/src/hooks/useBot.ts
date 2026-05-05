import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { botApi } from '../api/bot'

export function useBot() {
  const qc = useQueryClient()
  const [startError, setStartError] = useState<string | null>(null)

  const { data: status } = useQuery({
    queryKey: ['bot-status'],
    queryFn: botApi.status,
    refetchInterval: (q) => {
      const s = q.state.data?.state
      return s === 'running' || s === 'pending_review' || s === 'paused' ? 5000 : false
    },
    refetchIntervalInBackground: false,
  })

  const invalidate = () => qc.invalidateQueries({ queryKey: ['bot-status'] })

  const start = useMutation({
    mutationFn: (platform: string) => botApi.start(platform),
    onSuccess: () => { setStartError(null); invalidate() },
    onError: (err: Error) => setStartError(err.message),
  })

  const stop = useMutation({
    mutationFn: () => botApi.stop(),
    onSuccess: () => { setStartError(null); invalidate() },
  })

  const pause = useMutation({
    mutationFn: () => botApi.pause(),
    onSuccess: invalidate,
  })

  const resume = useMutation({
    mutationFn: () => botApi.resume(),
    onSuccess: invalidate,
  })

  return { status, start, stop, pause, resume, startError }
}
