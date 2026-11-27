import { Navigate } from 'react-router-dom'
import { useManualReview } from '../../hooks/useManualReview'

// Nested inside the authenticated route, so login/return-to routing runs first.
export function ManualReviewRoute({ children }: Readonly<{ children: React.ReactNode }>) {
  const review = useManualReview()
  if (review.loading) return <p role="status">Loading application settings…</p>
  if (review.isError) return (
    <div role="alert">
      Could not load application settings.
      <button type="button" onClick={() => void review.refetch()} className="ml-2 underline">Try again</button>
    </div>
  )
  if (!review.enabled) return <Navigate to="/" replace />
  return <>{children}</>
}
