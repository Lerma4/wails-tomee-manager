import {StrictMode} from 'react'
import {createRoot} from 'react-dom/client'
import './style.css'
import App from './App'

const container = document.getElementById('root')
if (!container) throw new Error('#root element not found in index.html')

const root = createRoot(container)

root.render(
    <StrictMode>
        <App/>
    </StrictMode>
)
