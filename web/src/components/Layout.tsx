import { useState } from 'react'
import { Sidebar } from './Sidebar'
import { BottomNav } from './BottomNav'
import { Outlet } from 'react-router-dom'

export function Layout() {
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem('sidebar-collapsed') === 'true'
  )

  function toggleCollapsed() {
    setCollapsed(c => {
      const next = !c
      localStorage.setItem('sidebar-collapsed', String(next))
      return next
    })
  }

  return (
    <div className="min-h-dvh bg-[var(--color-background)]">
      <Sidebar collapsed={collapsed} onToggle={toggleCollapsed} />
      <BottomNav />
      <main
        className={collapsed ? 'md:pl-14 pb-16 md:pb-0' : 'md:pl-56 pb-16 md:pb-0'}
        style={{ transition: 'padding-left 200ms' }}
      >
        <div className="max-w-5xl mx-auto px-4 py-6 md:px-6">
          <Outlet />
        </div>
      </main>
    </div>
  )
}
