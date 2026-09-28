import { useQuery } from '@tanstack/react-query'
import { plansApi, type PublicPlan } from '../api/plans'

const fallbackPlans: PublicPlan[] = [
  { id: 'trial', name: 'Trial', credits: 500, trial_days: 7, unit_amount: 0, configured: true },
  { id: 'starter', name: 'Starter', credits: 3000, configured: false },
  { id: 'pro', name: 'Pro', credits: 8000, configured: false },
]

export function usePublicPlans() {
  return useQuery({
    queryKey: ['public-plans'],
    queryFn: async () => (await plansApi.list()).plans,
    staleTime: 5 * 60 * 1000,
    retry: 1,
    placeholderData: fallbackPlans,
  })
}
