import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import { loadDemoServer } from './demo/demoLoader'
import { isDemoMode } from './demo/demoMode'
import './index.css'

async function bootstrap() {
  if (isDemoMode()) {
    await loadDemoServer()
  }
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  )
}

void bootstrap()
