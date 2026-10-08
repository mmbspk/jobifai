import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { Layout } from './components/Layout'
import { Dashboard } from './pages/Dashboard'
import { JobsApplied } from './pages/JobsApplied'
import { JobsSkipped } from './pages/JobsSkipped'
import { JobsCannotApply } from './pages/JobsCannotApply'
import { TopMatches } from './pages/TopMatches'
import { Review } from './pages/Review'
import { Generate } from './pages/Generate'
import { Documents } from './pages/Documents'
import { ApplicationSettingsPage } from './pages/settings/Application'
import { SecuritySettingsPage } from './pages/settings/Security'
import { AccountSettingsPage } from './pages/settings/Account'
import { AdminOverviewPage } from './pages/admin/Overview'
import { AdminAIProviderPage } from './pages/admin/AIProvider'
import { AdminDefaultsPage } from './pages/admin/Defaults'
import { AdminAutomationPage } from './pages/admin/Automation'
import { AdminUsersPage } from './pages/admin/Users'
import { AdminQuotaPage } from './pages/admin/Quota'
import { AdminRetentionPage } from './pages/admin/Retention'
import { AdminEconomicsPage } from './pages/admin/Economics'
import { AdminModelsPage } from './pages/admin/Models'
import { AdminLlmUsagePage } from './pages/admin/LlmUsage'
import { AdminAuditErrorsPage } from './pages/admin/AuditErrors'
import { Preferences } from './pages/settings/Preferences'
import { Resume } from './pages/settings/Resume'
import { PlatformsSettingsPage } from './pages/settings/Platforms'
import { PlanPage } from './pages/settings/Plan'
import { Login } from './pages/Login'
import { Register } from './pages/Register'
import { VerifyEmailPage } from './pages/VerifyEmail'
import { VerifyEmailChangePage } from './pages/VerifyEmailChange'
import { ForgotPassword } from './pages/ForgotPassword'
import { ResetPassword } from './pages/ResetPassword'
import { Landing } from './pages/Landing'
import { Pricing } from './pages/Pricing'
import { PostAuthRedirect } from './components/auth/PostAuthRedirect'
import { AuthProvider, useAuth } from './contexts/AuthContext'
import { hasStoredAuthCredentials } from './lib/authSession'
import { resolvePostAuthDestination, savePostAuthRedirect } from './lib/postAuthRedirect'
import { SettingsTabNav } from './components/settings/SettingsTabNav'
import { AdminNav } from './components/admin/AdminNav'
import { Badge } from './components/ui/badge'
import { PageHeader } from './components/shell/PageHeader'

const SETTINGS_TABS = [
  { to: '/settings/account',     label: 'Account' },
  { to: '/settings/application', label: 'Application' },
  { to: '/settings/preferences', label: 'Preferences' },
  { to: '/settings/resume',      label: 'Profile' },
  { to: '/settings/platforms',   label: 'Platforms' },
  { to: '/settings/plan',        label: 'Plan' },
  { to: '/settings/security',    label: 'Security' },
]

function SettingsLayout() {
  return (
    <div className="grid gap-6 lg:grid-cols-[190px_minmax(0,1fr)] lg:items-start">
      <div className="lg:sticky lg:top-8">
        <SettingsTabNav tabs={SETTINGS_TABS} />
      </div>
      <div className="min-w-0">
        <Routes>
          <Route path="application" element={<ApplicationSettingsPage />} />
          <Route path="account"     element={<AccountSettingsPage />} />
          <Route path="preferences" element={<Preferences />} />
          <Route path="resume"      element={<Resume />} />
          <Route path="platforms"   element={<PlatformsSettingsPage />} />
          <Route path="plan"        element={<PlanPage />} />
          <Route path="security"    element={<SecuritySettingsPage />} />
          <Route path="general"     element={<Navigate to="/settings/application" replace />} />
          <Route path="secrets"     element={<Navigate to="/settings/platforms" replace />} />
          <Route path="usage"       element={<Navigate to="/" replace />} />
          <Route index element={<Navigate to="application" replace />} />
        </Routes>
      </div>
    </div>
  )
}

function AdminLayout() {
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-3 border-b border-[var(--color-border-subtle)] pb-5">
        <div className="flex-1"><PageHeader title="Admin workspace" description="Monitor operations and manage Jobifai." /></div>
        <Badge variant="admin">Administrator</Badge>
      </div>
      <div className="grid min-w-0 gap-6 lg:grid-cols-[208px_minmax(0,1fr)] lg:gap-8">
        <AdminNav />
        <div className="min-w-0 space-y-6">
          <Routes>
            <Route path="overview"     element={<AdminOverviewPage />} />
            <Route path="ai-provider"  element={<AdminAIProviderPage />} />
            <Route path="defaults"     element={<AdminDefaultsPage />} />
            <Route path="automation"   element={<AdminAutomationPage />} />
            <Route path="users"        element={<AdminUsersPage />} />
            <Route path="quota"        element={<AdminQuotaPage />} />
            <Route path="retention"    element={<AdminRetentionPage />} />
            <Route path="economics"    element={<AdminEconomicsPage />} />
            <Route path="models"       element={<AdminModelsPage />} />
            <Route path="llm-usage"    element={<AdminLlmUsagePage />} />
            <Route path="audit"        element={<AdminAuditErrorsPage />} />
            <Route path="usage"        element={<Navigate to="/admin/llm-usage" replace />} />
            <Route path="system"       element={<Navigate to="/admin/ai-provider" replace />} />
            <Route path="secrets"      element={<Navigate to="/admin/ai-provider" replace />} />
            <Route index element={<Navigate to="overview" replace />} />
          </Routes>
        </div>
      </div>
    </div>
  )
}

// Protected URLs always sign in via login so we can return to the requested page.
function ProtectedRoute({ children }: Readonly<{ children: React.ReactNode }>) {
  const { user, loading } = useAuth()
  const location = useLocation()
  if (loading) return null
  if (!user) {
    savePostAuthRedirect(`${location.pathname}${location.search}${location.hash}`)
    return <Navigate to="/login" state={{ from: location }} replace />
  }
  return <>{children}</>
}

function AdminRoute({ children }: Readonly<{ children: React.ReactNode }>) {
  const { user, loading } = useAuth()
  const location = useLocation()
  if (loading) return null
  if (!user) {
    savePostAuthRedirect(`${location.pathname}${location.search}${location.hash}`)
    return <Navigate to="/login" state={{ from: location }} replace />
  }
  if (!user.is_admin) return <Navigate to="/" replace state={{ from: location }} />
  return <>{children}</>
}

type AuthRedirectState = { from?: { pathname: string; search?: string; hash?: string } }

/** Signed-in users leave auth pages for their intended destination (or home). */
function GuestRoute({ children }: Readonly<{ children: React.ReactNode }>) {
  const { user, loading } = useAuth()
  const location = useLocation()
  if (loading) return null
  if (user) {
    const dest = resolvePostAuthDestination((location.state as AuthRedirectState | null)?.from)
    return <Navigate to={dest} replace />
  }
  return <>{children}</>
}

/** App root: dashboard when signed in; welcome or login when not. */
function HomeGate() {
  const { user, loading } = useAuth()
  if (loading) return null
  if (user) return <Layout><Dashboard /></Layout>
  if (hasStoredAuthCredentials()) return <Navigate to="/login" replace />
  return <Landing />
}

/** Public welcome URL — same landing, redirect to app when already signed in. */
function WelcomePage() {
  const { user, loading } = useAuth()
  if (loading) return null
  if (user) return <Navigate to="/" replace />
  return <Landing />
}

function AppRoutes() {
  return (
    <Routes>
      {/* Public pages — kept outside the authenticated app shell. */}
      <Route path="/"        element={<HomeGate />} />
      <Route path="/welcome" element={<WelcomePage />} />
      <Route path="/pricing" element={<Pricing />} />
      <Route path="/login"    element={<GuestRoute><Login /></GuestRoute>} />
      <Route path="/register" element={<GuestRoute><Register /></GuestRoute>} />
      <Route path="/forgot-password" element={<GuestRoute><ForgotPassword /></GuestRoute>} />
      <Route path="/reset-password"  element={<ResetPassword />} />
      <Route path="/verify-email" element={<VerifyEmailPage />} />
      <Route path="/verify-email-change" element={<VerifyEmailChangePage />} />

      {/* Authenticated app shell (paths other than /) */}
      <Route element={<ProtectedRoute><Layout /></ProtectedRoute>}>
        <Route path="/jobs/applied"          element={<JobsApplied />} />
        <Route path="/jobs/skipped"          element={<JobsSkipped />} />
        <Route path="/jobs/cannot-apply"     element={<JobsCannotApply />} />
        <Route path="/jobs/top-matches"      element={<TopMatches />} />
        <Route path="/review"                element={<Review />} />
        <Route path="/generate"              element={<Generate />} />
        <Route path="/documents"            element={<Documents />} />
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
        <PostAuthRedirect />
        <AppRoutes />
      </AuthProvider>
    </BrowserRouter>
  )
}
