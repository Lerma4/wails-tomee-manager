import React, { useState, useEffect, useRef } from 'react';
import { Start, Stop, Restart } from '../../wailsjs/go/service/TomEEService';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { FaPlay, FaStop, FaRedo, FaCopy, FaCheck } from 'react-icons/fa';

const Dashboard: React.FC = () => {
    const [loading, setLoading] = useState(false);
    const [status, setStatus] = useState<'Running' | 'Stopped' | 'Unknown'>('Unknown');
    const [logs, setLogs] = useState<string[]>([]);
    const [copied, setCopied] = useState(false);
    const logsEndRef = useRef<HTMLDivElement>(null);

    const copyLogs = () => {
        navigator.clipboard.writeText(logs.join('\n')).then(() => {
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
        });
    };

    useEffect(() => {
        const cancelLogListener = EventsOn("tomee-log", (log: string) => {
            setLogs((prev) => {
                const newLogs = [...prev, log];
                if (newLogs.length > 1000) return newLogs.slice(-1000);
                return newLogs;
            });
        });
        return () => { cancelLogListener(); };
    }, []);

    useEffect(() => {
        logsEndRef.current?.scrollIntoView({ behavior: "smooth" });
    }, [logs]);

    const handleAction = async (actionName: string, actionFn: () => Promise<void>) => {
        setLoading(true);
        try {
            await actionFn();
            setStatus(actionName === 'Stop' ? 'Stopped' : 'Running');
        } catch (err) {
            console.error(err);
            alert(`Error during ${actionName}: ` + err);
        } finally {
            setLoading(false);
        }
    };

    const statusDotClass = status === 'Running' ? 'running' : status === 'Stopped' ? 'stopped' : 'unknown';

    return (
        <div className="p-6 h-screen flex flex-col overflow-hidden page-enter">
            {/* Header */}
            <div className="flex-none mb-6">
                <h1 className="text-2xl font-bold tracking-tight">Dashboard</h1>
                <p className="text-sm text-base-content/40 mt-1">Monitor and control your TomEE instance</p>
            </div>

            {/* Status & Actions Row */}
            <div className="flex-none grid grid-cols-1 md:grid-cols-3 gap-4 mb-4">
                {/* Status Card */}
                <div className="panel p-5">
                    <span className="form-label">Server Status</span>
                    <div className="flex items-center gap-3 mt-3">
                        <span className={`status-dot ${statusDotClass}`} />
                        <span className={`text-lg font-semibold tracking-tight ${
                            status === 'Running' ? 'text-success' :
                            status === 'Stopped' ? 'text-base-content/50' : 'text-warning'
                        }`}>
                            {status}
                        </span>
                    </div>
                </div>

                {/* Actions Card */}
                <div className="panel p-5 md:col-span-2">
                    <span className="form-label">Actions</span>
                    <div className="flex gap-3 mt-3">
                        <button
                            className="btn btn-success btn-sm gap-2"
                            onClick={() => handleAction('Start', Start)}
                            disabled={loading || status === 'Running'}
                        >
                            {/* @ts-ignore */}
                            {loading ? <span className="loading loading-spinner loading-xs" /> : <FaPlay className="text-xs" />}
                            Start
                        </button>
                        <button
                            className="btn btn-error btn-sm gap-2"
                            onClick={() => handleAction('Stop', Stop)}
                            disabled={loading || status !== 'Running'}
                        >
                            {/* @ts-ignore */}
                            {loading ? <span className="loading loading-spinner loading-xs" /> : <FaStop className="text-xs" />}
                            Stop
                        </button>
                        <button
                            className="btn btn-warning btn-sm gap-2"
                            onClick={() => handleAction('Restart', Restart)}
                            disabled={loading || status !== 'Running'}
                        >
                            {/* @ts-ignore */}
                            {loading ? <span className="loading loading-spinner loading-xs" /> : <FaRedo className="text-xs" />}
                            Restart
                        </button>
                    </div>
                </div>
            </div>

            {/* Terminal Log Viewer */}
            <div className="terminal flex-1 flex flex-col overflow-hidden">
                <div className="terminal-header">
                    <span className="terminal-dot" style={{ background: 'oklch(65% 0.22 25)' }} />
                    <span className="terminal-dot" style={{ background: 'oklch(82% 0.16 85)' }} />
                    <span className="terminal-dot" style={{ background: 'oklch(72% 0.17 155)' }} />
                    <span className="text-[0.7rem] font-mono text-base-content/30 ml-2 uppercase tracking-wider">
                        catalina.out
                    </span>
                    <button
                        className="ml-auto btn btn-ghost btn-xs gap-1 text-base-content/40 hover:text-base-content/70"
                        onClick={copyLogs}
                        disabled={logs.length === 0}
                        title="Copy logs to clipboard"
                    >
                        {copied ? <FaCheck className="text-success text-[0.6rem]" /> : <FaCopy className="text-[0.6rem]" />}
                        {copied ? 'Copied' : 'Copy'}
                    </button>
                </div>
                <div className="terminal-body flex-1 overflow-y-auto">
                    {logs.length === 0 && (
                        <div className="log-placeholder">Waiting for server output...</div>
                    )}
                    {logs.map((log, index) => (
                        <div key={index} className="log-line">{log}</div>
                    ))}
                    <div ref={logsEndRef} />
                </div>
            </div>
        </div>
    );
};

export default Dashboard;
