import { useBackend } from './BackendContext.tsx'
import { Login } from './Login.tsx'
import { NotAdmin } from './NotAdmin.tsx'
import { AppShell } from './AppShell.tsx'
import { Dashboard } from './Dashboard.tsx'

export function App() {
  const { session, loading, adminState } = useBackend()

  if (loading) {
    return null
  }

  if (!session) {
    return <Login />
  }

  if (adminState === 'checking') {
    return null
  }

  if (adminState === 'not-admin') {
    return <NotAdmin />
  }

  return (
    <AppShell>
      <Dashboard />
    </AppShell>
  )
}
