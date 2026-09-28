import { ChevronDown } from 'lucide-react'
import { PublicLayout } from '../components/marketing/PublicLayout'
import { PlanCard } from '../components/pricing/PlanCard'
import { usePublicPlans } from '../hooks/usePublicPlans'

const faqs = [
  {
    q: 'What are Jobifai credits?',
    a: 'Credits are used when Jobifai performs AI work such as role matching, application analysis, tailoring, and preparing application answers. Usage varies with the work required.',
  },
  {
    q: 'What happens when I run out?',
    a: 'Trial users can choose a paid plan. Subscribers can add extra credits or change plan. Jobifai shows your remaining balance before you reach the limit.',
  },
  {
    q: 'Do monthly credits roll over?',
    a: 'No. The included monthly allowance resets each billing period.',
  },
  {
    q: 'Do top-up credits roll over?',
    a: 'No. Extra credit packs are available for the current billing period and expire when that period ends.',
  },
  {
    q: 'Can I review applications before they are submitted?',
    a: 'Yes. Review-before-submit is a core Jobifai control and can be enabled so an application waits for your approval.',
  },
  {
    q: 'Which professions does Jobifai support?',
    a: 'Jobifai is designed around your profile, preferences, and target roles rather than a specific profession.',
  },
]

export function Pricing() {
  const { data: plans = [] } = usePublicPlans()

  return (
    <PublicLayout>
      <section className="mx-auto max-w-[1180px] px-4 pb-16 pt-20 text-center sm:px-6 sm:pb-20 sm:pt-24">
        <div className="mx-auto max-w-3xl">
          <p className="text-sm font-semibold text-[var(--color-accent)]">Simple pricing</p>
          <h1 className="mt-3 text-4xl font-bold tracking-[-0.045em] text-[var(--color-text)] sm:text-5xl">
            Start small. Scale with your job search.
          </h1>
          <p className="mx-auto mt-5 max-w-2xl text-base leading-7 text-[var(--color-text-muted)] sm:text-lg">
            Try the full workflow, then choose the monthly credit allowance that fits how actively you are applying.
            Core Jobifai controls stay available across plans.
          </p>
        </div>

        <div className="mx-auto mt-12 grid max-w-5xl gap-4 text-left md:grid-cols-3">
          {plans.map(plan => (
            <PlanCard key={plan.id} plan={plan} recommended={plan.id === 'pro'} />
          ))}
        </div>

        <p className="mt-5 text-xs text-[var(--color-text-dim)]">
          Paid-plan prices are shown from the configured Stripe billing catalog. Taxes, if applicable, are confirmed at checkout.
        </p>
      </section>

      <section className="border-y border-[var(--color-border-subtle)] bg-[var(--color-surface)]/35">
        <div className="mx-auto grid max-w-[980px] gap-8 px-4 py-14 sm:px-6 md:grid-cols-2 md:items-start">
          <div>
            <p className="text-sm font-semibold text-[var(--color-accent)]">Credits, without the guesswork</p>
            <h2 className="mt-2 text-2xl font-semibold tracking-tight">One balance for Jobifai's AI work.</h2>
          </div>
          <div className="space-y-3 text-sm leading-6 text-[var(--color-text-muted)]">
            <p>
              Credits cover AI-assisted work such as scoring roles, evaluating fit, tailoring resumes and cover letters,
              and preparing application answers.
            </p>
            <p>
              Jobifai keeps the balance visible in the app. Subscribers can buy extra credits for the current billing
              period without changing plan.
            </p>
          </div>
        </div>
      </section>

      <section className="mx-auto max-w-[880px] px-4 py-16 sm:px-6 sm:py-20">
        <div className="text-center">
          <p className="text-sm font-semibold text-[var(--color-accent)]">FAQ</p>
          <h2 className="mt-2 text-3xl font-semibold tracking-[-0.035em]">Before you subscribe</h2>
        </div>
        <div className="mt-10 divide-y divide-[var(--color-border-subtle)] rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] px-5 sm:px-6">
          {faqs.map(item => (
            <details key={item.q} className="group py-5">
              <summary className="flex cursor-pointer list-none items-center gap-4 text-left text-sm font-semibold text-[var(--color-text)]">
                <span className="flex-1">{item.q}</span>
                <ChevronDown size={16} className="text-[var(--color-text-dim)] transition-transform group-open:rotate-180" />
              </summary>
              <p className="max-w-2xl pt-3 text-sm leading-6 text-[var(--color-text-muted)]">{item.a}</p>
            </details>
          ))}
        </div>
      </section>
    </PublicLayout>
  )
}
