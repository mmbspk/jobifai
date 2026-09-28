import { ArrowRight, Check, CircleCheck, Search, ShieldCheck, Sparkles } from 'lucide-react'
import { Link } from 'react-router-dom'
import { PublicLayout } from '../components/marketing/PublicLayout'
import { PlanCard } from '../components/pricing/PlanCard'
import { usePublicPlans } from '../hooks/usePublicPlans'

export function Landing() {
  const { data: plans = [] } = usePublicPlans()

  return (
    <PublicLayout>
      <section className="relative overflow-hidden">
        <div className="pointer-events-none absolute inset-x-0 top-0 h-[520px] bg-[radial-gradient(ellipse_at_top,var(--color-accent-soft),transparent_68%)] opacity-70" />
        <div className="relative mx-auto grid max-w-[1180px] gap-12 px-4 pb-20 pt-16 sm:px-6 sm:pt-24 lg:grid-cols-[1.05fr_.95fr] lg:items-center lg:pb-28">
          <div>
            <div className="inline-flex items-center gap-2 rounded-full border border-[var(--color-accent)]/20 bg-[var(--color-accent-soft)]/50 px-3 py-1.5 text-xs font-medium text-[var(--color-accent)]">
              <Sparkles size={14} />
              Calm automation for your job search
            </div>
            <h1 className="mt-6 max-w-3xl text-5xl font-bold leading-[1.02] tracking-[-0.055em] text-[var(--color-text)] sm:text-6xl">
              Job applications, without the repetitive work.
            </h1>
            <p className="mt-6 max-w-2xl text-lg leading-8 text-[var(--color-text-muted)]">
              Jobifai finds relevant roles, evaluates your fit, prepares tailored application materials, and handles
              routine application steps while you stay in control of what gets submitted.
            </p>
            <div className="mt-8 flex flex-wrap gap-3">
              <Link to="/register" className="inline-flex min-h-[48px] items-center gap-2 rounded-[var(--radius-md)] bg-[var(--color-accent)] px-5 text-sm font-semibold text-white shadow-[var(--shadow-sm)] hover:bg-[var(--color-accent-hover)]">
                Start free <ArrowRight size={16} />
              </Link>
              <a href="#how-it-works" className="inline-flex min-h-[48px] items-center rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] px-5 text-sm font-medium text-[var(--color-text)] hover:bg-[var(--color-surface-2)]">
                See how it works
              </a>
            </div>
            <div className="mt-7 flex flex-wrap gap-x-5 gap-y-2 text-sm text-[var(--color-text-muted)]">
              {['Any profession', 'Review before submission', 'Transparent credit usage'].map(item => (
                <span key={item} className="inline-flex items-center gap-1.5">
                  <Check size={15} className="text-[var(--color-accent)]" /> {item}
                </span>
              ))}
            </div>
          </div>

          <div className="relative">
            <div className="rounded-[26px] border border-[var(--color-border)] bg-[var(--color-surface)] p-4 shadow-[var(--shadow-popover)] sm:p-5">
              <div className="flex items-center justify-between border-b border-[var(--color-border-subtle)] pb-4">
                <div>
                  <p className="text-xs text-[var(--color-text-dim)]">Automation</p>
                  <p className="mt-1 text-sm font-semibold">Searching your target roles</p>
                </div>
                <span className="inline-flex items-center gap-1.5 rounded-full bg-[var(--color-success-soft)] px-2.5 py-1 text-xs font-medium text-[var(--color-success)]">
                  <span className="h-1.5 w-1.5 rounded-full bg-current" /> Running
                </span>
              </div>

              <div className="py-5">
                <div className="flex items-start justify-between gap-4">
                  <div>
                    <p className="text-xs text-[var(--color-text-dim)]">Current match</p>
                    <h2 className="mt-1 text-xl font-semibold tracking-tight">Project Coordinator</h2>
                    <p className="mt-1 text-sm text-[var(--color-text-muted)]">Arcadia Health · Adelaide · Hybrid</p>
                  </div>
                  <span className="rounded-full bg-[var(--color-info-soft)] px-2.5 py-1 text-sm font-semibold text-[var(--color-info)]">8.9</span>
                </div>

                <div className="mt-5 space-y-2.5 rounded-[var(--radius-lg)] bg-[var(--color-surface-2)] p-4">
                  <p className="text-xs font-medium text-[var(--color-text-dim)]">Why this matches</p>
                  {['Relevant coordination experience', 'Preferred work arrangement', 'Strong skills overlap'].map(signal => (
                    <p key={signal} className="flex items-center gap-2 text-sm text-[var(--color-text-muted)]">
                      <CircleCheck size={15} className="text-[var(--color-success)]" /> {signal}
                    </p>
                  ))}
                </div>
              </div>

              <div className="flex items-center justify-between rounded-[var(--radius-lg)] border border-[var(--color-accent)]/25 bg-[var(--color-accent-soft)]/35 px-4 py-3">
                <div>
                  <p className="text-xs text-[var(--color-text-dim)]">Next step</p>
                  <p className="text-sm font-medium">Added to your review queue</p>
                </div>
                <ArrowRight size={17} className="text-[var(--color-accent)]" />
              </div>
            </div>
          </div>
        </div>
      </section>

      <section id="how-it-works" className="border-y border-[var(--color-border-subtle)] bg-[var(--color-surface)]/30">
        <div className="mx-auto max-w-[1180px] px-4 py-20 sm:px-6">
          <div className="max-w-2xl">
            <p className="text-sm font-semibold text-[var(--color-accent)]">How it works</p>
            <h2 className="mt-2 text-3xl font-semibold tracking-[-0.04em] sm:text-4xl">Automation with clear checkpoints.</h2>
          </div>
          <div className="mt-10 grid gap-4 md:grid-cols-3">
            {[
              { icon: Search, title: 'Find relevant roles', body: 'Jobifai searches supported job platforms using the roles, locations, and work modes you choose.' },
              { icon: Sparkles, title: 'Evaluate and prepare', body: 'AI scores the match and can tailor your resume, cover letter, and application answers to the role.' },
              { icon: ShieldCheck, title: 'Review or automate', body: 'Keep human review before submission, or configure the automation level that suits your workflow.' },
            ].map(item => (
              <article key={item.title} className="rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] p-6">
                <div className="flex h-11 w-11 items-center justify-center rounded-[var(--radius-md)] bg-[var(--color-accent-soft)] text-[var(--color-accent)]">
                  <item.icon size={20} />
                </div>
                <h3 className="mt-5 text-lg font-semibold">{item.title}</h3>
                <p className="mt-2 text-sm leading-6 text-[var(--color-text-muted)]">{item.body}</p>
              </article>
            ))}
          </div>
        </div>
      </section>

      <section id="features" className="mx-auto grid max-w-[1180px] gap-12 px-4 py-20 sm:px-6 lg:grid-cols-2 lg:items-center">
        <div>
          <p className="text-sm font-semibold text-[var(--color-accent)]">You remain the decision-maker</p>
          <h2 className="mt-2 text-3xl font-semibold tracking-[-0.04em] sm:text-4xl">Review what matters before it goes anywhere.</h2>
          <p className="mt-5 max-w-xl text-base leading-7 text-[var(--color-text-muted)]">
            Review score reasoning, application materials, platform details, and employment-ethics flags before approving a submission.
            Automation saves repetitive work without hiding the important decisions.
          </p>
        </div>

        <div className="rounded-[var(--radius-xl)] border border-[var(--color-border)] bg-[var(--color-surface)] p-5 shadow-[var(--shadow-card)] sm:p-6">
          <div className="flex items-start justify-between gap-4">
            <div>
              <p className="text-xs text-[var(--color-text-dim)]">Review application</p>
              <h3 className="mt-1 text-lg font-semibold">Operations Manager</h3>
              <p className="mt-1 text-sm text-[var(--color-text-muted)]">Ridgeway Group · Melbourne</p>
            </div>
            <span className="rounded-full bg-[var(--color-info-soft)] px-2.5 py-1 text-sm font-semibold text-[var(--color-info)]">8.7 Strong</span>
          </div>
          <div className="mt-5 grid gap-3 sm:grid-cols-2">
            <div className="rounded-[var(--radius-md)] bg-[var(--color-surface-2)] p-4">
              <p className="text-xs text-[var(--color-text-dim)]">Documents</p>
              <p className="mt-2 text-sm font-medium">Tailored resume</p>
              <p className="mt-1 text-sm font-medium">Cover letter</p>
            </div>
            <div className="rounded-[var(--radius-md)] bg-[var(--color-warn-soft)] p-4">
              <p className="text-xs text-[var(--color-text-dim)]">Employment ethics</p>
              <p className="mt-2 text-sm font-medium text-[var(--color-warn)]">Potential concern</p>
              <p className="mt-1 text-xs leading-5 text-[var(--color-text-muted)]">Open the reasoning before deciding.</p>
            </div>
          </div>
          <div className="mt-5 flex justify-end gap-2 border-t border-[var(--color-border-subtle)] pt-5">
            <span className="inline-flex h-10 items-center rounded-[var(--radius-md)] border border-[var(--color-border)] px-4 text-sm text-[var(--color-text-muted)]">Reject</span>
            <span className="inline-flex h-10 items-center rounded-[var(--radius-md)] bg-[var(--color-accent)] px-4 text-sm font-semibold text-white">Approve application</span>
          </div>
        </div>
      </section>

      <section className="border-y border-[var(--color-border-subtle)] bg-[var(--color-surface)]/30">
        <div className="mx-auto max-w-[1180px] px-4 py-20 sm:px-6">
          <div className="flex flex-wrap items-end justify-between gap-5">
            <div>
              <p className="text-sm font-semibold text-[var(--color-accent)]">Simple pricing</p>
              <h2 className="mt-2 text-3xl font-semibold tracking-[-0.04em]">Start with a trial. Upgrade when you need more.</h2>
            </div>
            <Link to="/pricing" className="inline-flex items-center gap-2 text-sm font-semibold text-[var(--color-accent)] hover:underline">
              Compare plans <ArrowRight size={15} />
            </Link>
          </div>
          <div className="mt-9 grid gap-4 md:grid-cols-3">
            {plans.map(plan => <PlanCard key={plan.id} plan={plan} recommended={plan.id === 'pro'} compact />)}
          </div>
        </div>
      </section>

      <section className="mx-auto max-w-[900px] px-4 py-20 text-center sm:px-6 sm:py-24">
        <h2 className="text-3xl font-semibold tracking-[-0.04em] sm:text-4xl">Spend less time repeating application work.</h2>
        <p className="mx-auto mt-4 max-w-2xl text-base leading-7 text-[var(--color-text-muted)]">
          Set up your profile and preferences, connect a supported platform, and let Jobifai take care of the repetitive steps.
        </p>
        <Link to="/register" className="mt-7 inline-flex min-h-[48px] items-center gap-2 rounded-[var(--radius-md)] bg-[var(--color-accent)] px-5 text-sm font-semibold text-white hover:bg-[var(--color-accent-hover)]">
          Start free <ArrowRight size={16} />
        </Link>
      </section>
    </PublicLayout>
  )
}
