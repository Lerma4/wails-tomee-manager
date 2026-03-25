import { HashRouter as Router, Routes, Route } from 'react-router-dom';
import Sidebar from './components/Sidebar';
import Dashboard from './pages/Dashboard';
import WarManager from './pages/WarManager';
import Configuration from './pages/Configuration';
import './style.css';

function App() {
    return (
        <Router>
            <div className="flex h-screen bg-base-300">
                <Sidebar />
                <main className="flex-1 overflow-y-auto bg-base-100">
                    <Routes>
                        <Route path="/" element={<Dashboard />} />
                        <Route path="/wars" element={<WarManager />} />
                        <Route path="/config" element={<Configuration />} />
                    </Routes>
                </main>
            </div>
        </Router>
    );
}

export default App;
