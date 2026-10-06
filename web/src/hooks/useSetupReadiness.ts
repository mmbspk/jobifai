import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { authApi } from '../api/auth'
import { settingsApi } from '../api/settings'
import { documentsApi } from '../api/documents'
import { ApiError } from '../api/client'
import { quotaApi } from '../api/quota'
import { QUOTA_QUERY_KEY } from './useQuota'
import {
  evaluateSetupSteps,
  firstIncompleteStep,
  requiredSetupComplete,
  setupProgress,
  type EvaluatedSetupStep,
} from '../lib/setupChecklist'

async function fetchResumeProfile() {
  try {
    return await settingsApi.resume.get()
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null
    throw e
  }
}

export function useSetupReadiness() {
  const profileQ = useQuery({
    queryKey: ['settings-resume'],
    queryFn: fetchResumeProfile,
    staleTime: 30_000,
  })
  const prefsQ = useQuery({
    queryKey: ['settings-preferences'],
    queryFn: settingsApi.preferences.get,
    staleTime: 30_000,
  })
  const linkedInQ = useQuery({
    queryKey: ['platform-session', 'linkedin'],
    queryFn: () => authApi.platformStatus('linkedin'),
    staleTime: 30_000,
  })
  const seekQ = useQuery({
    queryKey: ['platform-session', 'seek'],
    queryFn: () => authApi.platformStatus('seek'),
    staleTime: 30_000,
  })
  const quotaQ = useQuery({
    queryKey: QUOTA_QUERY_KEY,
    queryFn: quotaApi.status,
    staleTime: 30_000,
  })
  const documentsQ = useQuery({
    queryKey: ['documents'],
    queryFn: documentsApi.list,
    staleTime: 30_000,
  })

  const [planReviewTick, setPlanReviewTick] = useState(0)
  useEffect(() => {
    const bump = () => setPlanReviewTick(n => n + 1)
    window.addEventListener('jobifai:setup-plan-reviewed', bump)
    return () => window.removeEventListener('jobifai:setup-plan-reviewed', bump)
  }, [])

  const loading =
    profileQ.isLoading || prefsQ.isLoading || linkedInQ.isLoading || seekQ.isLoading || quotaQ.isLoading || documentsQ.isLoading

  const steps: EvaluatedSetupStep[] = useMemo(
    () =>
      evaluateSetupSteps({
        profile: profileQ.data,
        preferences: prefsQ.data,
        linkedInSession: linkedInQ.data?.has_session === true,
        seekSession: seekQ.data?.has_session === true,
        quotaUnlimited: quotaQ.data?.unlimited === true,
        hasDefaultDocument: Boolean(documentsQ.data?.defaults?.resume_version_id),
      }),
    [
      profileQ.data,
      prefsQ.data,
      linkedInQ.data?.has_session,
      seekQ.data?.has_session,
      quotaQ.data?.unlimited,
      documentsQ.data?.defaults?.resume_version_id,
      planReviewTick,
    ],
  )

  const ready = requiredSetupComplete(steps)
  const progress = setupProgress(steps)
  const nextStep = firstIncompleteStep(steps)

  function refresh() {
    void profileQ.refetch()
    void prefsQ.refetch()
    void linkedInQ.refetch()
    void seekQ.refetch()
    void quotaQ.refetch()
    void documentsQ.refetch()
  }

  return { steps, ready, progress, nextStep, loading, refresh }
}
