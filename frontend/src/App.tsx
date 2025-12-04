import { useState } from 'react';
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
                <div className="flex-1 overflow-y-auto">
                    <Routes>
                        <Route path="/" element={<Dashboard />} />
                        <Route path="/wars" element={<WarManager />} />
                        <Route path="/config" element={<Configuration />} />
                    </Routes>
                </div>
            </div>
        </Router>
    );
}

export default App;
