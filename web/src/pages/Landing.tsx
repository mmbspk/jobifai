import { Link } from 'react-router-dom'
import { ApplicationPreview } from '../components/marketing/ApplicationPreview'
import { PublicLayout } from '../components/marketing/PublicLayout'
import { usePublicPlans } from '../hooks/usePublicPlans'
import './Landing.css'

export function Landing() {
  const { data: plans = [] } = usePublicPlans()
  const trial = plans.find(plan => plan.id === 'trial')

  return (
    <PublicLayout className="welcome-page">
      <section className="welcome-hero welcome-container" aria-labelledby="welcome-heading">
        <div className="welcome-intro">
          <h1 id="welcome-heading">The job search<br />is a job in itself.</h1>
          <p className="welcome-lead">
            Let Jobifai help with the legwork. Find roles that fit your experience,
            prepare your documents and decide what gets sent.
          </p>
          <div className="welcome-actions">
            <Link to="/register" className="welcome-button">Start free</Link>
            <a href="#how-it-works" className="welcome-text-link">See how it works</a>
          </div>
          <p className="welcome-platforms">Job search and applications for any profession.</p>
        </div>
        <ApplicationPreview />
      </section>

      <div className="welcome-platform-bar welcome-container">
        <p>Works with <strong>LinkedIn</strong> and <strong>SEEK</strong></p>
        <p>Already found a role? Bring a job URL or description.</p>
      </div>

      <section id="how-it-works" className="welcome-workflow welcome-container" aria-labelledby="workflow-heading">
        <div className="welcome-section-intro">
          <h2 id="workflow-heading">A little setup.<br />A lot less repetition.</h2>
          <p>Start with your experience and what you want from your next role. Jobifai takes it from there.</p>
        </div>
        <ol className="welcome-steps">
          <li>
            <h3>Tell it what you’re looking for</h3>
            <p>
              Upload your resume, review your profile and choose your target roles and locations.
              Connect your LinkedIn or SEEK account to start a search.
            </p>
          </li>
          <li>
            <h3>Get the fit, then the paperwork</h3>
            <p>
              AI scores each role against your profile and explains the match. Reuse your saved
              resume and cover letter, or have them tailored to the job.
            </p>
          </li>
          <li>
            <h3>Review it. Make your move.</h3>
            <p>
              Prepare and preview the documents before you approve. Jobifai can submit supported
              applications, including LinkedIn Easy Apply. Strong matches that need a manual
              application stay in Top Matches.
            </p>
          </li>
        </ol>
      </section>

      <section id="features" className="welcome-features" aria-labelledby="features-heading">
        <div className="welcome-container">
          <div className="welcome-features-heading">
            <h2 id="features-heading">Your documents.<br />Ready for the next role.</h2>
            <p>Your profile, your documents and the work you’ve already put in, together in one place.</p>
          </div>
          <div className="welcome-feature-grid">
            <article>
              <h3>A document library that keeps your work</h3>
              <p>
                Upload originals, edit saved resume and cover letter versions, and choose your defaults.
                Use regional resume styles and set the tone of your cover letters. Preview and download
                the PDFs when you need them.
              </p>
            </article>
            <article>
              <h3>Help with the role you found yourself</h3>
              <p>
                Add a job URL or paste a description in Generate. Check your fit, tailor a resume,
                write a cover letter or prepare answers to application questions.
              </p>
            </article>
            <article>
              <h3>Your own rules for applying</h3>
              <p>
                Set a minimum match score and a daily application limit. Keep review-before-submit
                enabled or choose automatic submission. An optional Halal job filter checks roles
                against Islamic employment ethics.
              </p>
            </article>
            <article>
              <h3>A clear record of what happened</h3>
              <p>
                Track applied, skipped and manual-step jobs, with run activity that survives a refresh.
                See credit usage broken down into AI, browser automation and document rendering.
              </p>
            </article>
          </div>
        </div>
      </section>

      <section className="welcome-start welcome-container" aria-labelledby="start-heading">
        <div>
          <h2 id="start-heading">Start with your resume.</h2>
          <p>Create an account, add your profile and choose your first search.</p>
          {trial && (
            <p className="welcome-trial">
              Your free trial includes {trial.credits.toLocaleString()} credits
              {trial.trial_days ? ` to use over ${trial.trial_days} days` : ''}.
              {' '}Credits cover AI work, browser automation and PDF rendering.
            </p>
          )}
        </div>
        <div className="welcome-start-actions">
          <Link to="/register" className="welcome-button">Start free</Link>
          <Link to="/pricing" className="welcome-text-link">Compare plans</Link>
        </div>
      </section>
    </PublicLayout>
  )
}
