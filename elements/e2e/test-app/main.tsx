import React from 'react'
import ReactDOM from 'react-dom/client'
import { TestFixture } from './fixtures'
import '../../src/global.css'

// Get fixture name from URL params
const params = new URLSearchParams(window.location.search)
const fixture = params.get('fixture') || 'welcome'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <TestFixture name={fixture} />
  </React.StrictMode>
)
