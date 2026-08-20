import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { IsRunning, OpenInBrowser, Restart, Start, Stop } from '../../wailsjs/go/service/TomEEService';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { type Level, clearLogs, useLogs } from '../logStore';
import {
    FaArrowDown,
    FaCheck,
    FaChevronDown,
    FaChevronUp,
    FaCopy,
    FaExclamationTriangle,
    FaExternalLinkAlt,
    FaPlay,
    FaRedo,
    FaSearch,
    FaStop,
    FaTrash,
} from 'react-icons/fa';

/** Starting is its own state: the process exists long before the server answers. */
type ServerStatus = 'Running' | 'Starting' | 'Stopped' | 'Unknown';

const LEVELS: Level[] = ['ERROR', 'WARN', 'INFO', 'DEBUG'];
const LEVEL_LABEL: Record<Level, string> = { ERROR: 'Errors', WARN: 'Warnings', INFO: 'Info', DEBUG: 'Debug' };

/** A record longer than this is clamped until the user opens it. */
const CLAMP_LINES = 6;

/** Wraps every occurrence of query in <mark> so matches are visible without scrolling. */
const Highlighted = ({ text, query }: { text: string; query: string }) => {
    if (!query) return <>{text}</>;

    const needle = query.toLowerCase();
    const haystack = text.toLowerCase();
    const parts: React.ReactNode[] = [];
    let cursor = 0;
    let key = 0;

    for (let idx = haystack.indexOf(needle); idx !== -1; idx = haystack.indexOf(needle, cursor)) {
        if (idx > cursor) parts.push(text.slice(cursor, idx));
        parts.push(
            <mark key={key++} className="log-mark">
                {text.slice(idx, idx + needle.length)}
            </mark>,
        );
        cursor = idx + needle.length;
    }
    if (cursor === 0) return <>{text}</>;
    parts.push(text.slice(cursor));
    return <>{parts}</>;
};

const Dashboard = () => {
    const [loading, setLoading] = useState(false);
    const [status, setStatus] = useState<ServerStatus>('Unknown');
    const entries = useLogs();
    const [copied, setCopied] = useState(false);

    const [hidden, setHidden] = useState<Set<Level>>(new Set());
    const [query, setQuery] = useState('');
    const [matchIndex, setMatchIndex] = useState(0);
    const [expanded, setExpanded] = useState<Set<number>>(new Set());
    const [pendingJump, setPendingJump] = useState(false);

    // Following the tail is opt-out: the moment the user scrolls up to read
    // something, new output must stop yanking the view away.
    const [following, setFollowing] = useState(true);

    const bodyRef = useRef<HTMLDivElement>(null);
    const rowRefs = useRef<(HTMLDivElement | null)[]>([]);

    /* ---------- server status ---------- */

    useEffect(() => {
        const poll = () => {
            IsRunning()
                .then((running) =>
                    setStatus((prev) => {
                        // IsRunning only reports that the process exists, so it must not
                        // upgrade Starting to Running: the log event does that.
                        if (prev === 'Starting' && running) return 'Starting';
                        return running ? 'Running' : 'Stopped';
                    }),
                )
                .catch(() => setStatus('Unknown'));
        };
        poll();
        const interval = setInterval(poll, 3000);
        // The backend reports the boot phases as they happen; the poll above is
        // only a backstop, and the one that notices a server started elsewhere.
        const cancel = EventsOn('tomee-status', (state: string) => {
            if (state === 'starting') return setStatus('Starting');
            return setStatus(state === 'running' ? 'Running' : 'Stopped');
        });
        return () => {
            clearInterval(interval);
            cancel();
        };
    }, []);

    /* ---------- filtering ---------- */

    const counts = useMemo(() => {
        const tally: Record<Level, number> = { ERROR: 0, WARN: 0, INFO: 0, DEBUG: 0 };
        for (const entry of entries) tally[entry.level] = (tally[entry.level] ?? 0) + 1;
        return tally;
    }, [entries]);

    const visible = useMemo(() => {
        const needle = query.trim().toLowerCase();
        return entries.filter(
            (entry) => !hidden.has(entry.level) && (needle === '' || entry.text.toLowerCase().includes(needle)),
        );
    }, [entries, hidden, query]);

    const toggleLevel = (level: Level) => {
        setHidden((prev) => {
            const next = new Set(prev);
            if (next.has(level)) {
                next.delete(level);
            } else {
                next.add(level);
            }
            return next;
        });
    };

    const showEverything = () => setHidden(new Set<Level>());

    /* ---------- navigation ---------- */

    const scrollToRow = useCallback((index: number) => {
        rowRefs.current[index]?.scrollIntoView({ block: 'center' });
    }, []);

    const step = (delta: number) => {
        if (visible.length === 0) return;
        const next = (matchIndex + delta + visible.length) % visible.length;
        setFollowing(false);
        setMatchIndex(next);
        scrollToRow(next);
    };

    /** Narrows the console to errors and warnings, then lands on the newest one. */
    const showOnlyProblems = () => {
        setHidden(new Set<Level>(['INFO', 'DEBUG']));
        setFollowing(false);
        setPendingJump(true);
    };

    // Runs after the filter above has been applied, so `visible` already holds
    // just the problems and its last entry is the most recent one.
    useEffect(() => {
        if (!pendingJump) return;
        setPendingJump(false);
        const last = visible.length - 1;
        if (last < 0) return;
        setMatchIndex(last);
        scrollToRow(last);
    }, [pendingJump, visible, scrollToRow]);

    /* ---------- tail following ---------- */

    // biome-ignore lint/correctness/useExhaustiveDependencies: `entries` is the trigger; the effect only reads the DOM node
    useEffect(() => {
        if (!following) return;
        const body = bodyRef.current;
        // Jumping straight to the bottom, not scrollIntoView: smooth scrolling
        // cannot keep up with a booting server and ends up lagging behind.
        if (body) body.scrollTop = body.scrollHeight;
    }, [entries, following]);

    const onScroll = () => {
        const body = bodyRef.current;
        if (!body) return;
        const atBottom = body.scrollHeight - body.scrollTop - body.clientHeight < 40;
        setFollowing(atBottom);
    };

    const resumeFollowing = () => {
        setFollowing(true);
        const body = bodyRef.current;
        if (body) body.scrollTop = body.scrollHeight;
    };

    /* ---------- actions ---------- */

    const copyLogs = () => {
        navigator.clipboard.writeText(visible.map((entry) => entry.text).join('\n')).then(() => {
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
        });
    };

    const handleClear = () => {
        clearLogs();
        setExpanded(new Set());
        setMatchIndex(0);
    };

    const toggleExpanded = (id: number) => {
        setExpanded((prev) => {
            const next = new Set(prev);
            if (next.has(id)) {
                next.delete(id);
            } else {
                next.add(id);
            }
            return next;
        });
    };

    const handleAction = async (actionName: string, actionFn: () => Promise<void>) => {
        setLoading(true);
        try {
            await actionFn();
        } catch (err) {
            console.error(err);
            alert(`Error during ${actionName}: ${err}`);
        } finally {
            setLoading(false);
        }
    };

    const statusDotClass =
        status === 'Running' ? 'running' : status === 'Starting' ? 'unknown' : status === 'Stopped' ? 'stopped' : 'unknown';
    const isUp = status === 'Running' || status === 'Starting';
    const problems = counts.ERROR + counts.WARN;
    const searching = query.trim() !== '';

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
                        <span
                            className={`text-lg font-semibold tracking-tight ${
                                status === 'Running'
                                    ? 'text-success'
                                    : status === 'Stopped'
                                      ? 'text-base-content/50'
                                      : 'text-warning'
                            }`}
                        >
                            {status}
                        </span>
                        {status === 'Starting' && <span className="loading loading-spinner loading-xs" />}
                    </div>
                </div>

                {/* Actions Card */}
                <div className="panel p-5 md:col-span-2">
                    <span className="form-label">Actions</span>
                    <div className="flex gap-3 mt-3 flex-wrap">
                        <button
                            type="button"
                            className="btn btn-success btn-sm gap-2"
                            onClick={() => handleAction('Start', Start)}
                            disabled={loading || isUp}
                        >
                            {loading ? <span className="loading loading-spinner loading-xs" /> : <FaPlay className="text-xs" />}
                            Start
                        </button>
                        <button
                            type="button"
                            className="btn btn-error btn-sm gap-2"
                            onClick={() => handleAction('Stop', Stop)}
                            disabled={loading || !isUp}
                        >
                            {loading ? <span className="loading loading-spinner loading-xs" /> : <FaStop className="text-xs" />}
                            Stop
                        </button>
                        <button
                            type="button"
                            className="btn btn-warning btn-sm gap-2"
                            onClick={() => handleAction('Restart', Restart)}
                            disabled={loading || !isUp}
                        >
                            {loading ? <span className="loading loading-spinner loading-xs" /> : <FaRedo className="text-xs" />}
                            Restart
                        </button>
                        <button
                            type="button"
                            className="btn btn-ghost btn-sm gap-2"
                            onClick={() => handleAction('Open in browser', OpenInBrowser)}
                            disabled={status !== 'Running'}
                            title="Open the server root in your browser"
                        >
                            <FaExternalLinkAlt className="text-xs" />
                            Open
                        </button>
                    </div>
                </div>
            </div>

            {/* Terminal Log Viewer */}
            <div className="terminal flex-1 flex flex-col overflow-hidden">
                <div className="terminal-header flex-wrap">
                    <span className="terminal-dot" style={{ background: 'oklch(65% 0.22 25)' }} />
                    <span className="terminal-dot" style={{ background: 'oklch(82% 0.16 85)' }} />
                    <span className="terminal-dot" style={{ background: 'oklch(72% 0.17 155)' }} />
                    <span className="text-[0.7rem] font-mono text-base-content/30 ml-2 uppercase tracking-wider">
                        catalina.out
                    </span>

                    {/* Level filters — the count is the point: a silent ERROR badge is
                        the thing that used to scroll past unnoticed. */}
                    <div className="flex items-center gap-1 ml-3">
                        {LEVELS.map((level) => (
                            <button
                                type="button"
                                key={level}
                                className={`log-chip log-chip-${level.toLowerCase()} ${hidden.has(level) ? 'is-off' : ''}`}
                                onClick={() => toggleLevel(level)}
                                title={`${hidden.has(level) ? 'Show' : 'Hide'} ${LEVEL_LABEL[level].toLowerCase()}`}
                            >
                                {level}
                                <span className="log-chip-count">{counts[level]}</span>
                            </button>
                        ))}
                    </div>

                    <button
                        type="button"
                        className={`btn btn-xs gap-1 ${problems > 0 ? 'btn-error' : 'btn-ghost'}`}
                        onClick={showOnlyProblems}
                        disabled={problems === 0}
                        title="Show only errors and warnings"
                    >
                        <FaExclamationTriangle className="text-[0.6rem]" />
                        {problems}
                    </button>
                    {hidden.size > 0 && (
                        <button type="button" className="btn btn-ghost btn-xs" onClick={showEverything}>
                            Show all
                        </button>
                    )}

                    {/* Search */}
                    <div className="flex items-center gap-1 ml-auto">
                        <div className="log-search">
                            <FaSearch className="text-[0.65rem] text-base-content/30" />
                            <input
                                type="text"
                                value={query}
                                placeholder="Search logs"
                                onChange={(e) => {
                                    setQuery(e.target.value);
                                    setMatchIndex(0);
                                }}
                                onKeyDown={(e) => {
                                    if (e.key === 'Enter') step(e.shiftKey ? -1 : 1);
                                }}
                            />
                            {searching && (
                                <span className="text-[0.65rem] text-base-content/40 font-mono whitespace-nowrap">
                                    {visible.length === 0 ? '0/0' : `${matchIndex + 1}/${visible.length}`}
                                </span>
                            )}
                        </div>
                        <button
                            type="button"
                            className="btn btn-ghost btn-xs px-1"
                            onClick={() => step(-1)}
                            disabled={visible.length === 0}
                            title="Previous match (Shift+Enter)"
                        >
                            <FaChevronUp className="text-[0.6rem]" />
                        </button>
                        <button
                            type="button"
                            className="btn btn-ghost btn-xs px-1"
                            onClick={() => step(1)}
                            disabled={visible.length === 0}
                            title="Next match (Enter)"
                        >
                            <FaChevronDown className="text-[0.6rem]" />
                        </button>

                        <button
                            type="button"
                            className="btn btn-ghost btn-xs gap-1 text-base-content/40 hover:text-base-content/70"
                            onClick={copyLogs}
                            disabled={visible.length === 0}
                            title="Copy the visible logs"
                        >
                            {copied ? <FaCheck className="text-success text-[0.6rem]" /> : <FaCopy className="text-[0.6rem]" />}
                            {copied ? 'Copied' : 'Copy'}
                        </button>
                        <button
                            type="button"
                            className="btn btn-ghost btn-xs px-1 text-base-content/40 hover:text-base-content/70"
                            onClick={handleClear}
                            disabled={entries.length === 0}
                            title="Clear the console"
                        >
                            <FaTrash className="text-[0.6rem]" />
                        </button>
                    </div>
                </div>

                <div className="terminal-body flex-1 overflow-y-auto relative" ref={bodyRef} onScroll={onScroll}>
                    {entries.length === 0 && <div className="log-placeholder">Waiting for server output...</div>}
                    {entries.length > 0 && visible.length === 0 && (
                        <div className="log-placeholder">No records match the current filter.</div>
                    )}
                    {visible.map((entry, index) => {
                        const lines = entry.text.split('\n');
                        const isOpen = expanded.has(entry.id);
                        const clamped = !isOpen && lines.length > CLAMP_LINES;
                        const shown = clamped ? lines.slice(0, 3) : lines;
                        return (
                            <div
                                key={entry.id}
                                ref={(el) => {
                                    rowRefs.current[index] = el;
                                }}
                                className={`log-entry log-entry-${entry.level.toLowerCase()} ${
                                    searching && index === matchIndex ? 'is-current' : ''
                                }`}
                            >
                                <span className="log-entry-level">{entry.level}</span>
                                <div className="log-entry-text">
                                    <Highlighted text={shown.join('\n')} query={query.trim()} />
                                    {lines.length > CLAMP_LINES && (
                                        <button
                                            type="button"
                                            className="log-entry-more"
                                            onClick={() => toggleExpanded(entry.id)}
                                        >
                                            {clamped ? `▸ ${lines.length - 3} more lines` : '▾ collapse'}
                                        </button>
                                    )}
                                </div>
                            </div>
                        );
                    })}
                </div>

                {!following && (
                    <button type="button" className="log-resume" onClick={resumeFollowing}>
                        <FaArrowDown className="text-[0.6rem]" />
                        Paused — jump to latest
                    </button>
                )}
            </div>
        </div>
    );
};

export default Dashboard;
