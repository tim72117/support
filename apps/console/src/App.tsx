import { useState } from 'react'
import { useMockBackend } from './MockBackendContext.tsx'
import { Login } from './Login.tsx'
import { AppShell } from './AppShell.tsx'
import { BusinessList } from './BusinessList.tsx'
import { BusinessEditor } from './BusinessEditor.tsx'

// No router library — the console only ever has three screens deep, so a
// small explicit state machine is easier to follow than wiring up routes
// for it. If this grows (settings page, team members, …) revisit.
type View = { name: 'list' } | { name: 'business'; businessId: string }

export function App() {
  const { session } = useMockBackend()
  const [view, setView] = useState<View>({ name: 'list' })

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
          businessId={view.businessId}
          onBack={() => setView({ name: 'list' })}
        />
      )}
    </AppShell>
  )
}
