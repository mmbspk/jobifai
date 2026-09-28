import { apiGet } from './client'

export interface PublicPlan {
  id: 'trial' | 'starter' | 'pro'
  name: string
  credits: number
  trial_days?: number
  unit_amount?: number
  currency?: string
  interval?: string
  configured: boolean
}

export interface PublicPlansResponse {
  plans: PublicPlan[]
}

export const plansApi = {
  list: () => apiGet<PublicPlansResponse>('/public/plans'),
}
