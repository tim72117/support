import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './style.css'
import { App } from './App.tsx'
import { MockBackendProvider } from './MockBackendContext.tsx'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <MockBackendProvider>
      <App />
    </MockBackendProvider>
  </StrictMode>,
)
