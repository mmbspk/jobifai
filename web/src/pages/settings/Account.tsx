import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../../contexts/AuthContext'
import { userApi } from '../../api/user'
import { Button } from '../../components/Button'
import { Input } from '../../components/ui/input'
import { PageHeader } from '../../components/shell/PageHeader'

export function AccountSettingsPage() {
  const { user, updateMe, forceLogout } = useAuth()
  const navigate = useNavigate()

  // Profile section
  const [displayName, setDisplayName] = useState(user?.display_name ?? '')
  const [avatarURL, setAvatarURL] = useState(user?.avatar_url ?? '')
  const [profileBusy, setProfileBusy] = useState(false)
  const [profileError, setProfileError] = useState('')
  const [profileSuccess, setProfileSuccess] = useState(false)

  // Email change section
  const [newEmail, setNewEmail] = useState('')
  const [emailPassword, setEmailPassword] = useState('')
  const [emailBusy, setEmailBusy] = useState(false)
  const [emailError, setEmailError] = useState('')
  const [emailSuccess, setEmailSuccess] = useState(false)

  // Delete account section
  const [deletePassword, setDeletePassword] = useState('')
  const [deleteConfirm, setDeleteConfirm] = useState('')
  const [deleteBusy, setDeleteBusy] = useState(false)
  const [deleteError, setDeleteError] = useState('')

  const isGoogleOnly = user?.has_google && !user?.has_password

  async function handleProfileSubmit(e: React.FormEvent) {
    e.preventDefault()
    setProfileError('')
    setProfileSuccess(false)
    if (!displayName.trim()) {
      setProfileError('Display name is required')
      return
    }
    setProfileBusy(true)
    try {
      await updateMe(displayName.trim(), avatarURL.trim())
      setProfileSuccess(true)
    } catch (err) {
      setProfileError(err instanceof Error ? err.message : 'Profile update failed')
    } finally {
      setProfileBusy(false)
    }
  }

  async function handleEmailSubmit(e: React.FormEvent) {
    e.preventDefault()
    setEmailError('')
    setEmailSuccess(false)
    if (!newEmail.trim()) {
      setEmailError('New email address is required')
      return
    }
    if (!emailPassword) {
      setEmailError('Current password is required')
      return
    }
    setEmailBusy(true)
    try {
      await userApi.requestEmailChange(newEmail.trim().toLowerCase(), emailPassword)
      setEmailSuccess(true)
      setNewEmail('')
      setEmailPassword('')
    } catch (err) {
      setEmailError(err instanceof Error ? err.message : 'Email change request failed')
    } finally {
      setEmailBusy(false)
    }
  }

  async function handleDeleteSubmit(e: React.FormEvent) {
    e.preventDefault()
    setDeleteError('')
    setDeleteBusy(true)
    try {
      if (isGoogleOnly) {
        if (deleteConfirm !== 'DELETE') {
          setDeleteError('You must type DELETE to confirm')
          setDeleteBusy(false)
          return
        }
        await userApi.deleteAccount({ confirm: 'DELETE' })
      } else {
        if (!deletePassword) {
          setDeleteError('Current password is required')
          setDeleteBusy(false)
          return
        }
        await userApi.deleteAccount({ currentPassword: deletePassword })
      }
      forceLogout()
      void navigate('/', { replace: true })
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : 'Account deletion failed')
    } finally {
      setDeleteBusy(false)
    }
  }

  return (
    <div className="space-y-10">
      <PageHeader title="Account" description="Manage your profile and account." />

      {/* Profile */}
      <section className="space-y-4">
        <h2 className="text-base font-semibold">Profile</h2>
        <form onSubmit={handleProfileSubmit} className="space-y-4 max-w-sm">
          {profileSuccess && (
            <p className="text-sm text-green-700 bg-green-50 border border-green-200 rounded-[var(--radius-md)] px-3 py-2 dark:text-green-300 dark:bg-green-950/40 dark:border-green-800/40" role="status">
              Profile updated.
            </p>
          )}
          {profileError && (
            <p className="text-sm text-red-700 bg-red-50 border border-red-200 rounded-[var(--radius-md)] px-3 py-2 dark:text-red-300 dark:bg-red-950/40 dark:border-red-800/40" role="alert">
              {profileError}
            </p>
          )}
          <div className="space-y-1">
            <label className="text-sm font-medium" htmlFor="display-name">Display name</label>
            <Input
              id="display-name"
              value={displayName}
              onChange={e => { setDisplayName(e.target.value); setProfileSuccess(false) }}
              placeholder="Your name"
              autoComplete="name"
            />
          </div>
          <div className="space-y-1">
            <label className="text-sm font-medium" htmlFor="avatar-url">Avatar URL <span className="text-[var(--color-text-muted)] font-normal">(optional)</span></label>
            <Input
              id="avatar-url"
              value={avatarURL}
              onChange={e => { setAvatarURL(e.target.value); setProfileSuccess(false) }}
              placeholder="https://example.com/avatar.jpg"
              autoComplete="off"
              type="url"
            />
          </div>
          <Button type="submit" disabled={profileBusy}>{profileBusy ? 'Saving…' : 'Save profile'}</Button>
        </form>
      </section>

      {/* Email change */}
      {!isGoogleOnly && (
        <section className="space-y-4">
          <h2 className="text-base font-semibold">Email address</h2>
          <p className="text-sm text-[var(--color-text-muted)]">
            Current: <span className="font-medium text-[var(--color-text)]">{user?.email}</span>
            {user?.pending_email && (
              <span className="ml-2 text-amber-600 dark:text-amber-400">(verification pending for {user.pending_email})</span>
            )}
          </p>
          {emailSuccess ? (
            <div className="rounded-[var(--radius-md)] border border-green-200 bg-green-50 px-4 py-3 text-sm text-green-800 dark:border-green-800/40 dark:bg-green-950/40 dark:text-green-300" role="status">
              Verification email sent — check your new inbox and click the link to confirm the change.
            </div>
          ) : (
            <form onSubmit={handleEmailSubmit} className="space-y-4 max-w-sm">
              {emailError && (
                <p className="text-sm text-red-700 bg-red-50 border border-red-200 rounded-[var(--radius-md)] px-3 py-2 dark:text-red-300 dark:bg-red-950/40 dark:border-red-800/40" role="alert">
                  {emailError}
                </p>
              )}
              <div className="space-y-1">
                <label className="text-sm font-medium" htmlFor="new-email">New email address</label>
                <Input
                  id="new-email"
                  type="email"
                  value={newEmail}
                  onChange={e => setNewEmail(e.target.value)}
                  placeholder="new@example.com"
                  autoComplete="email"
                />
              </div>
              <div className="space-y-1">
                <label className="text-sm font-medium" htmlFor="email-password">Current password</label>
                <Input
                  id="email-password"
                  type="password"
                  value={emailPassword}
                  onChange={e => setEmailPassword(e.target.value)}
                  autoComplete="current-password"
                />
              </div>
              <Button type="submit" disabled={emailBusy}>{emailBusy ? 'Sending…' : 'Request email change'}</Button>
            </form>
          )}
        </section>
      )}

      {isGoogleOnly && (
        <section className="space-y-2">
          <h2 className="text-base font-semibold">Email address</h2>
          <p className="text-sm text-[var(--color-text-muted)]">
            This account uses Google sign-in. Email changes must be managed through your Google account.
          </p>
        </section>
      )}

      {/* Danger zone */}
      <section className="space-y-4">
        <h2 className="text-base font-semibold text-red-600 dark:text-red-400">Danger zone</h2>
        <div className="rounded-[var(--radius-md)] border border-red-200 bg-red-50/50 p-6 space-y-4 dark:border-red-900/40 dark:bg-red-950/20">
          <div>
            <p className="text-sm font-medium text-[var(--color-text)]">Delete account</p>
            <p className="text-sm text-[var(--color-text-muted)] mt-1">
              Permanently delete your account and all associated data. This action cannot be undone.
            </p>
          </div>
          <form onSubmit={handleDeleteSubmit} className="space-y-4 max-w-sm">
            {deleteError && (
              <p className="text-sm text-red-700 bg-red-50 border border-red-200 rounded-[var(--radius-md)] px-3 py-2 dark:text-red-300 dark:bg-red-950/40 dark:border-red-800/40" role="alert">
                {deleteError}
              </p>
            )}
            {isGoogleOnly ? (
              <div className="space-y-1">
                <label className="text-sm font-medium" htmlFor="delete-confirm">
                  Type <strong>DELETE</strong> to confirm
                </label>
                <Input
                  id="delete-confirm"
                  value={deleteConfirm}
                  onChange={e => setDeleteConfirm(e.target.value)}
                  placeholder="DELETE"
                  autoComplete="off"
                />
              </div>
            ) : (
              <div className="space-y-1">
                <label className="text-sm font-medium" htmlFor="delete-password">Current password</label>
                <Input
                  id="delete-password"
                  type="password"
                  value={deletePassword}
                  onChange={e => setDeletePassword(e.target.value)}
                  autoComplete="current-password"
                />
              </div>
            )}
            <Button type="submit" variant="danger" disabled={deleteBusy}>
              {deleteBusy ? 'Deleting…' : 'Delete my account'}
            </Button>
          </form>
        </div>
      </section>
    </div>
  )
}
