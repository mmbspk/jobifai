import { BrowserRouter, NavLink, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { Layout } from './components/Layout'
import { Dashboard } from './pages/Dashboard'
import { JobsApplied } from './pages/JobsApplied'
import { JobsSkipped } from './pages/JobsSkipped'
import { JobsCannotApply } from './pages/JobsCannotApply'
import { TopMatches } from './pages/TopMatches'
import { Review } from './pages/Review'
import { Generate } from './pages/Generate'
import { GeneralSettingsPage } from './pages/settings/General'
import { Preferences } from './pages/settings/Preferences'
import { Resume } from './pages/settings/Resume'
import { Secrets } from './pages/settings/Secrets'
import { Login } from './pages/Login'
import { Register } from './pages/Register'
import { AuthProvider, useAuth } from './contexts/AuthContext'
import { cn } from './lib'

const SETTINGS_TABS = [
  { to: '/settings/general',     label: 'General' },
  { to: '/settings/preferences', label: 'Preferences' },
  { to: '/settings/resume',      label: 'Profile' },
  { to: '/settings/secrets',     label: 'Secrets' },
]

function SettingsLayout() {
  return (
    <div className="space-y-5">
      <div className="flex gap-1 border-b border-[var(--color-border)] overflow-x-auto">
        {SETTINGS_TABS.map(t => (
          <NavLink
            key={t.to}
            to={t.to}
            className={({ isActive }) =>
              cn(
                'px-4 py-2 text-sm border-b-2 -mb-px transition-colors whitespace-nowrap',
                isActive
                  ? 'border-violet-500 text-violet-300'
                  : 'border-transparent text-[var(--color-text-dim)] hover:text-[var(--color-text-muted)]',
              )
            }
          >
            {t.label}
          </NavLink>
        ))}
      </div>
      <Routes>
        <Route path="general"     element={<GeneralSettingsPage />} />
        <Route path="preferences" element={<Preferences />} />
        <Route path="resume"      element={<Resume />} />
        <Route path="secrets"     element={<Secrets />} />
        <Route index element={<Navigate to="general" replace />} />
      </Routes>
    </div>
  )
}

// Redirects unauthenticated users to /login; shows nothing while auth is loading.
function ProtectedRoute({ children }: Readonly<{ children: React.ReactNode }>) {
  const { user, loading } = useAuth()
  const location = useLocation()
  if (loading) return null
  if (!user) return <Navigate to="/login" state={{ from: location }} replace />
  return <>{children}</>
}

function AppRoutes() {
  return (
    <Routes>
      {/* Public auth pages */}
      <Route path="/login"    element={<Login />} />
      <Route path="/register" element={<Register />} />

      {/* All other routes require authentication */}
      <Route element={<ProtectedRoute><Layout /></ProtectedRoute>}>
        <Route path="/"                      element={<Dashboard />} />
        <Route path="/jobs/applied"          element={<JobsApplied />} />
        <Route path="/jobs/skipped"          element={<JobsSkipped />} />
        <Route path="/jobs/cannot-apply"     element={<JobsCannotApply />} />
        <Route path="/jobs/top-matches"      element={<TopMatches />} />
        <Route path="/review"                element={<Review />} />
        <Route path="/generate"              element={<Generate />} />
        <Route path="/settings/*"            element={<SettingsLayout />} />
        <Route path="*"                      element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

export function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <AppRoutes />
      </AuthProvider>
    </BrowserRouter>
  )
}
