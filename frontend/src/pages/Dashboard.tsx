import React, { useState, useEffect, useRef } from 'react';
import { Start, Stop, Restart } from '../../wailsjs/go/service/TomEEService';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { FaPlay, FaStop, FaRedo } from 'react-icons/fa';

const Dashboard: React.FC = () => {
    const [loading, setLoading] = useState(false);
    const [status, setStatus] = useState('Unknown');
    const [logs, setLogs] = useState<string[]>([]);
    const logsEndRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        // Listen for log events
        const cancelLogListener = EventsOn("tomee-log", (log: string) => {
            setLogs((prev) => {
                const newLogs = [...prev, log];
                if (newLogs.length > 1000) return newLogs.slice(-1000); // Keep last 1000 lines
                return newLogs;
            });
        });

        return () => {
            cancelLogListener();
        };
    }, []);

    // Auto-scroll to bottom of logs
    useEffect(() => {
        logsEndRef.current?.scrollIntoView({ behavior: "smooth" });
    }, [logs]);

    const handleAction = async (actionName: string, actionFn: () => Promise<void>) => {
        setLoading(true);
        try {
            await actionFn();
            setStatus(actionName === 'Start' ? 'Running' : actionName === 'Stop' ? 'Stopped' : 'Running');
        } catch (err) {
            console.error(err);
            alert(`Error during ${actionName}: ` + err);
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="p-6 h-screen flex flex-col overflow-hidden">
            <h1 className="text-3xl font-bold mb-6 flex-none">Dashboard</h1>

            <div className="grid grid-cols-1 md:grid-cols-3 gap-6 flex-none mb-6">
                <div className="card bg-base-100 shadow-xl">
                    <div className="card-body">
                        <h2 className="card-title">Server Status</h2>
                        <div className={`badge badge-lg ${status === 'Running' ? 'badge-success' : 'badge-ghost'}`}>{status}</div>
                    </div>
                </div>

                <div className="card bg-base-100 shadow-xl col-span-2">
                    <div className="card-body">
                        <h2 className="card-title">Actions</h2>
                        <div className="flex gap-4">
                            <button 
                                className={`btn btn-success gap-2 ${loading ? 'loading' : ''}`}
                                onClick={() => handleAction('Start', Start)}
                            >
                                {/* @ts-ignore */}
                                <FaPlay /> Start
                            </button>
                            <button 
                                className={`btn btn-error gap-2 ${loading ? 'loading' : ''}`}
                                onClick={() => handleAction('Stop', Stop)}
                            >
                                {/* @ts-ignore */}
                                <FaStop /> Stop
                            </button>
                            <button 
                                className={`btn btn-warning gap-2 ${loading ? 'loading' : ''}`}
                                onClick={() => handleAction('Restart', Restart)}
                            >
                                {/* @ts-ignore */}
                                <FaRedo /> Restart
                            </button>
                        </div>
                    </div>
                </div>
            </div>

            {/* Logs */}
            <div className="card bg-base-100 shadow-xl flex-1 overflow-hidden">
                <div className="card-body flex flex-col h-full p-0">
                    <h2 className="card-title p-4 pb-0">Logs</h2>
                    <div className="mockup-code flex-1 overflow-y-auto m-4 mt-2 bg-[#2a303c] text-sm">
                        {logs.length === 0 && <pre className="text-gray-500 p-4">No logs yet...</pre>}
                        {logs.map((log, index) => (
                            <pre key={index} data-prefix=">"><code>{log}</code></pre>
                        ))}
                        <div ref={logsEndRef} />
                    </div>
                </div>
            </div>
        </div>
    );
};

export default Dashboard;
