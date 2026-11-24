import * as Tabs from '@radix-ui/react-tabs'
import { Check, FileText } from 'lucide-react'

export function ApplicationPreview() {
  return (
    <figure className="welcome-example">
      <figcaption>Explore an example application</figcaption>
      <Tabs.Root defaultValue="match">
        <Tabs.List className="welcome-example-tabs" aria-label="Application steps">
          <Tabs.Trigger value="match">Job fit</Tabs.Trigger>
          <Tabs.Trigger value="documents">Documents</Tabs.Trigger>
          <Tabs.Trigger value="review">Review</Tabs.Trigger>
        </Tabs.List>

        <Tabs.Content value="match" className="welcome-example-sheet">
          <div className="welcome-example-role">
            <div>
              <p className="welcome-example-context">LinkedIn Easy Apply</p>
              <h2>Project Coordinator</h2>
              <p>Community health, Melbourne</p>
            </div>
            <div className="welcome-example-score" aria-label="Match score 8.7 out of 10">
              <strong>8.7</strong><span>out of 10</span>
            </div>
          </div>
          <div className="welcome-example-reasoning">
            <h3>Why it could be a good fit</h3>
            <ul>
              <li><Check size={16} aria-hidden="true" />Your project planning experience fits the role.</li>
              <li><Check size={16} aria-hidden="true" />Hybrid work matches your preferences.</li>
            </ul>
            <p className="welcome-example-note">Worth a closer look — the role asks for experience in healthcare.</p>
          </div>
          <p className="welcome-example-footnote">Set the minimum score for your own search.</p>
        </Tabs.Content>

        <Tabs.Content value="documents" className="welcome-example-sheet">
          <p className="welcome-example-context">Documents for this role</p>
          <h2>Your experience, on paper.</h2>
          <div className="welcome-example-document">
            <FileText size={22} aria-hidden="true" />
            <div>
              <h3>Project coordination resume</h3>
              <p>Version 3</p>
            </div>
            <span>Tailored</span>
          </div>
          <blockquote className="welcome-example-excerpt">
            “I bring hands-on experience coordinating projects, keeping teams organised
            and turning plans into practical next steps.”
          </blockquote>
          <div className="welcome-example-cover">
            <FileText size={18} aria-hidden="true" />
            <span>Cover letter prepared for this role</span>
          </div>
          <p className="welcome-example-footnote">Choose saved versions or tailor documents for each job.</p>
        </Tabs.Content>

        <Tabs.Content value="review" className="welcome-example-sheet">
          <p className="welcome-example-context">Review before submission</p>
          <h2>You have the final say.</h2>
          <ul className="welcome-example-checklist">
            <li><Check size={17} aria-hidden="true" /><span>Job details checked</span></li>
            <li><Check size={17} aria-hidden="true" /><span>Resume and cover letter prepared</span></li>
            <li><Check size={17} aria-hidden="true" /><span>Documents ready to preview</span></li>
          </ul>
          <div className="welcome-example-hold">
            <span className="welcome-status-dot" aria-hidden="true" />
            Waiting for your approval
          </div>
          <p className="welcome-example-footnote">With review enabled, the application waits until you approve it.</p>
        </Tabs.Content>
      </Tabs.Root>
    </figure>
  )
}
