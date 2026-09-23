import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { Layout } from './components/Layout'
import { Dashboard } from './pages/Dashboard'
import { JobsApplied } from './pages/JobsApplied'
import { JobsSkipped } from './pages/JobsSkipped'
import { JobsCannotApply } from './pages/JobsCannotApply'
import { TopMatches } from './pages/TopMatches'
import { Review } from './pages/Review'
import { Generate } from './pages/Generate'
import { ApplicationSettingsPage } from './pages/settings/Application'
import { AdminDefaultsPage } from './pages/admin/Defaults'
import { AdminAutomationPage } from './pages/admin/Automation'
import { AdminUsersPage } from './pages/admin/Users'
import { Preferences } from './pages/settings/Preferences'
import { Resume } from './pages/settings/Resume'
import { PlatformsSettingsPage } from './pages/settings/Platforms'
import { UsagePage } from './pages/settings/Usage'
import { Login } from './pages/Login'
import { Register } from './pages/Register'
import { AuthProvider, useAuth } from './contexts/AuthContext'
import { SettingsTabNav } from './components/settings/SettingsTabNav'
import { AdminTabNav } from './components/admin/AdminTabNav'
import { Badge } from './components/ui/badge'
import { PageHeader } from './components/shell/PageHeader'

const SETTINGS_TABS = [
  { to: '/settings/application', label: 'Application' },
  { to: '/settings/preferences', label: 'Preferences' },
  { to: '/settings/resume',      label: 'Profile' },
  { to: '/settings/platforms',   label: 'Platforms' },
]

const ADMIN_TABS = [
  { to: '/admin/defaults',   label: 'Defaults' },
  { to: '/admin/automation', label: 'Automation' },
  { to: '/admin/users',      label: 'Users' },
  { to: '/admin/usage',      label: 'Usage' },
]

function SettingsLayout() {
  return (
    <div className="space-y-6">
      <SettingsTabNav tabs={SETTINGS_TABS} />
      <Routes>
        <Route path="application" element={<ApplicationSettingsPage />} />
        <Route path="preferences" element={<Preferences />} />
        <Route path="resume"      element={<Resume />} />
        <Route path="platforms"   element={<PlatformsSettingsPage />} />
        <Route path="general"     element={<Navigate to="/settings/application" replace />} />
        <Route path="secrets"     element={<Navigate to="/settings/platforms" replace />} />
        <Route path="usage"       element={<Navigate to="/" replace />} />
        <Route index element={<Navigate to="application" replace />} />
      </Routes>
    </div>
  )
}

function AdminLayout() {
  return (
    <div className="space-y-6">
      <div className="space-y-3">
        <Badge variant="admin">Admin</Badge>
        <PageHeader
          title="System"
          description="Deployment configuration — only visible to administrators."
        />
      </div>
      <AdminTabNav tabs={ADMIN_TABS} />
      <Routes>
        <Route path="defaults"   element={<AdminDefaultsPage />} />
        <Route path="automation" element={<AdminAutomationPage />} />
        <Route path="users"      element={<AdminUsersPage />} />
        <Route path="usage"      element={<UsagePage />} />
        <Route path="system"     element={<Navigate to="/admin/defaults" replace />} />
        <Route path="secrets"    element={<Navigate to="/admin/defaults" replace />} />
        <Route index element={<Navigate to="defaults" replace />} />
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

function AdminRoute({ children }: Readonly<{ children: React.ReactNode }>) {
  const { user, loading } = useAuth()
  const location = useLocation()
  if (loading) return null
  if (!user) return <Navigate to="/login" state={{ from: location }} replace />
  if (!user.is_admin) return <Navigate to="/" replace state={{ from: location }} />
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
        <Route path="/admin/*"               element={<AdminRoute><AdminLayout /></AdminRoute>} />
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
