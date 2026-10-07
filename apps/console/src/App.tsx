import { useState } from 'react'
import { useBackend } from './BackendContext.tsx'
import { Login } from './Login.tsx'
import { AppShell } from './AppShell.tsx'
import { BusinessList } from './BusinessList.tsx'
import { BusinessEditor } from './BusinessEditor.tsx'

// No router library — the console only ever has three screens deep, so a
// small explicit state machine is easier to follow than wiring up routes
// for it. If this grows (settings page, team members, …) revisit.
type View = { name: 'list' } | { name: 'business'; businessId: number }

export function App() {
  const { session, loading } = useBackend()
  const [view, setView] = useState<View>({ name: 'list' })

  if (loading) {
    return null
  }

  if (!session) {
    return <Login />
  }

  return (
    <AppShell
      onNavigateHome={() => setView({ name: 'list' })}
      showBackButton={view.name === 'business'}
    >
      {view.name === 'list' && (
        <BusinessList onOpenBusiness={(id) => setView({ name: 'business', businessId: id })} />
      )}
      {view.name === 'business' && (
        <BusinessEditor
          key={view.businessId}
          businessId={view.businessId}
          onBack={() => setView({ name: 'list' })}
        />
      )}
    </AppShell>
  )
}
