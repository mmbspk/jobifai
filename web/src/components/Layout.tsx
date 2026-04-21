import { Sidebar } from './Sidebar'
import { BottomNav } from './BottomNav'
import { Outlet } from 'react-router-dom'

export function Layout() {
  return (
    <div className="min-h-dvh bg-[var(--color-background)]">
      <Sidebar />
      <BottomNav />
      <main className="md:pl-56 pb-16 md:pb-0">
        <div className="max-w-5xl mx-auto px-4 py-6 md:px-6">
          <Outlet />
        </div>
      </main>
    </div>
  )
}
