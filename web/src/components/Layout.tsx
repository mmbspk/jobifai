import { useState } from 'react'
import { Sidebar } from './Sidebar'
import { BottomNav } from './BottomNav'
import { Outlet } from 'react-router-dom'
import { cn } from '../lib'

export function Layout() {
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem('sidebar-collapsed') === 'true',
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
        className={cn(
          'max-md:pb-[calc(4rem+env(safe-area-inset-bottom))]',
          collapsed ? 'md:pl-16 lg:pl-14' : 'md:pl-16 lg:pl-56',
        )}
        style={{ transition: 'padding-left 200ms' }}
      >
        <div className="max-w-[1024px] mx-auto px-4 py-6 md:px-6 lg:py-8">
          <Outlet />
        </div>
      </main>
    </div>
  )
}
