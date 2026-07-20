import React from 'react'
import ReactDOM from 'react-dom/client'
import './index.css'
import { bootBuilding } from './buildingStore'

// Two-stage boot: fetch the engine's building geometry BEFORE the app module graph is
// imported. Root (and everything under it) derives module-scope constants from the
// geometry, so it must not evaluate until the store holds the live building — otherwise
// a blueprint deployed at runtime would render with the geometry compiled into the
// bundle. If the engine is unreachable the store keeps its bundled fallback and the app
// still boots.
bootBuilding().finally(async () => {
  const { default: Root } = await import('./Root.jsx')
  ReactDOM.createRoot(document.getElementById('root')).render(
    <React.StrictMode>
      <Root />
    </React.StrictMode>,
  )
})
