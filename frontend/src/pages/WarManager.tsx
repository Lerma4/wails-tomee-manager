import { useEffect, useRef, useState } from 'react';
import { ListWars, SaveWar, DeleteWar } from '../../wailsjs/go/service/StorageService';
import { DeployAll as DeployAllWars } from '../../wailsjs/go/service/WarService';
import { CheckWarExists, RunBuild } from '../../wailsjs/go/service/MavenService';
import { SelectWarFile } from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { model } from '../../wailsjs/go/models';
import { FaPlus, FaTrash, FaEdit, FaRocket, FaFolder, FaBoxOpen, FaSync, FaCheckCircle, FaTimesCircle, FaHammer, FaFileAlt } from 'react-icons/fa';

type BuildState = 'idle' | 'building' | 'success' | 'error';

/* ------------------------------------------------------------------ */
/*  BuildLogModal                                                      */
/* ------------------------------------------------------------------ */

interface BuildLogModalProps {
    warId: number;
    wars: model.WarArtifact[];
    buildStates: Record<number, BuildState>;
    buildLogs: Record<number, string[]>;
    onClose: () => void;
}

const BuildLogModal = ({ warId, wars, buildStates, buildLogs, onClose }: BuildLogModalProps) => {
    const logsEndRef = useRef<HTMLDivElement>(null);
    const war = wars.find((w) => w.id === warId);
    const state = buildStates[warId] || 'idle';
    const lines = buildLogs[warId] || [];

    useEffect(() => {
        logsEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    }, [lines]);

    const badgeClass =
        state === 'building' ? 'badge-info' :
        state === 'success'  ? 'badge-success' :
        state === 'error'    ? 'badge-error' :
        'badge-ghost';

    const badgeLabel =
        state === 'building' ? 'Building...' :
        state === 'success'  ? 'Success' :
        state === 'error'    ? 'Error' :
        'Idle';

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center modal-backdrop-blur">
            <div className="panel p-6 w-full max-w-3xl mx-4 flex flex-col" style={{ maxHeight: '80vh' }}>
                {/* Header */}
                <div className="flex items-center justify-between mb-4">
                    <div className="flex items-center gap-3">
                        <h3 className="text-lg font-bold tracking-tight">
                            Build Logs — {war?.destName || `WAR #${warId}`}
                        </h3>
                        <span className={`badge badge-sm ${badgeClass}`}>
                            {state === 'building' && <span className="loading loading-spinner loading-xs mr-1" />}
                            {badgeLabel}
                        </span>
                    </div>
                    <button className="btn btn-ghost btn-sm" onClick={onClose}>Close</button>
                </div>

                {/* Log area */}
                <div className="terminal-body flex-1 overflow-y-auto">
                    {lines.length === 0 && (
                        <div className="log-placeholder">Waiting for build output...</div>
                    )}
                    {lines.map((line, idx) => (
                        <div key={idx} className="log-line">{line}</div>
                    ))}
                    <div ref={logsEndRef} />
                </div>
            </div>
        </div>
    );
};

/* ------------------------------------------------------------------ */
/*  WarManager                                                         */
/* ------------------------------------------------------------------ */

const WarManager = () => {
    const [wars, setWars] = useState<model.WarArtifact[]>([]);
    const [deploying, setDeploying] = useState(false);
    const [modalOpen, setModalOpen] = useState(false);
    const [currentWar, setCurrentWar] = useState<model.WarArtifact>(new model.WarArtifact());

    // Task 5 — WAR existence check
    const [warExistsMap, setWarExistsMap] = useState<Record<number, boolean | null>>({});

    // Task 6 — Maven build per-row
    const [buildStates, setBuildStates] = useState<Record<number, BuildState>>({});
    const [buildLogs, setBuildLogs] = useState<Record<number, string[]>>({});
    const [logModalWarId, setLogModalWarId] = useState<number | null>(null);
    const [refreshing, setRefreshing] = useState(false);
    const buildTimers = useRef<Map<number, ReturnType<typeof setTimeout>>>(new Map());

    /* ---------- WAR existence helpers ---------- */

    const checkWarExists = (war: model.WarArtifact) => {
        setWarExistsMap((prev) => ({ ...prev, [war.id]: null }));
        CheckWarExists(war.sourcePath)
            .then((exists) => setWarExistsMap((prev) => ({ ...prev, [war.id]: exists })))
            .catch(() => setWarExistsMap((prev) => ({ ...prev, [war.id]: false })));
    };

    const checkAllWarExists = (warList: model.WarArtifact[]) => {
        warList.forEach((w) => checkWarExists(w));
    };

    /* ---------- Fetch & lifecycle ---------- */

    const fetchWars = () => {
        ListWars()
            .then((data) => {
                const list = data || [];
                setWars(list);
                checkAllWarExists(list);
            })
            .catch(console.error);
    };

    useEffect(() => { fetchWars(); }, []);

    /* ---------- Event listeners for Maven build ---------- */

    const warsRef = useRef<model.WarArtifact[]>([]);
    useEffect(() => { warsRef.current = wars; }, [wars]);

    const activeListeners = useRef<Map<number, () => void>>(new Map());

    // Incremental listener management: register new, unregister removed
    useEffect(() => {
        const currentIds = new Set(wars.map((w) => w.id));
        const registeredIds = activeListeners.current;

        // Register listeners for new WAR IDs
        wars.forEach((war) => {
            if (registeredIds.has(war.id)) return;

            const cancelLog = EventsOn(`maven-log-${war.id}`, (line: string) => {
                setBuildLogs((prev) => ({
                    ...prev,
                    [war.id]: [...(prev[war.id] || []), line],
                }));
            });

            const cancelDone = EventsOn(`maven-done-${war.id}`, (result: { success: boolean; error: string }) => {
                const newState: BuildState = result.success ? 'success' : 'error';
                setBuildStates((prev) => ({ ...prev, [war.id]: newState }));

                if (!result.success && result.error) {
                    setBuildLogs((prev) => ({
                        ...prev,
                        [war.id]: [...(prev[war.id] || []), `BUILD FAILED: ${result.error}`],
                    }));
                }

                // Re-check WAR existence for this artifact
                const w = warsRef.current.find((x) => x.id === war.id);
                if (w) checkWarExists(w);

                // Reset to idle after 3 seconds
                const prevTimer = buildTimers.current.get(war.id);
                if (prevTimer) clearTimeout(prevTimer);
                const timer = setTimeout(() => {
                    setBuildStates((prev) => ({ ...prev, [war.id]: 'idle' }));
                    buildTimers.current.delete(war.id);
                }, 3000);
                buildTimers.current.set(war.id, timer);
            });

            registeredIds.set(war.id, () => { cancelLog(); cancelDone(); });
        });

        // Unregister listeners for removed WAR IDs
        registeredIds.forEach((cleanup, id) => {
            if (!currentIds.has(id)) {
                cleanup();
                registeredIds.delete(id);
            }
        });
    }, [wars]);

    // Full teardown on unmount only
    useEffect(() => {
        return () => {
            activeListeners.current.forEach((cleanup) => cleanup());
            activeListeners.current.clear();
            buildTimers.current.forEach((timer) => clearTimeout(timer));
            buildTimers.current.clear();
        };
    }, []);

    /* ---------- Build handler ---------- */

    const handleBuild = async (warId: number) => {
        setBuildStates((prev) => ({ ...prev, [warId]: 'building' }));
        setBuildLogs((prev) => ({ ...prev, [warId]: [] }));
        try {
            await RunBuild(warId);
        } catch (err) {
            setBuildStates((prev) => ({ ...prev, [warId]: 'error' }));
            setBuildLogs((prev) => ({
                ...prev,
                [warId]: [...(prev[warId] || []), `Build failed: ${err}`],
            }));
            const prev = buildTimers.current.get(warId);
            if (prev) clearTimeout(prev);
            const timer = setTimeout(() => {
                setBuildStates((p) => ({ ...p, [warId]: 'idle' }));
                buildTimers.current.delete(warId);
            }, 3000);
            buildTimers.current.set(warId, timer);
        }
    };

    /* ---------- CRUD handlers ---------- */

    const handleSave = async () => {
        try {
            await SaveWar(currentWar);
            setModalOpen(false);
            fetchWars();
        } catch (err) {
            console.error(err);
            alert('Error saving WAR: ' + err);
        }
    };

    const handleDelete = async (id: number) => {
        if (!confirm('Are you sure you want to delete this WAR artifact?')) return;
        try {
            await DeleteWar(id);
            if (logModalWarId === id) setLogModalWarId(null);
            fetchWars();
        } catch (err) { console.error(err); }
    };

    const handleDeploy = async () => {
        setDeploying(true);
        try {
            await DeployAllWars();
            alert('Deployment successful!');
        } catch (err) {
            alert('Deployment failed: ' + err);
        } finally {
            setDeploying(false);
        }
    };

    const openModal = (war?: model.WarArtifact) => {
        setCurrentWar(war ? { ...war } : new model.WarArtifact());
        setModalOpen(true);
    };

    /* ---------- Build column button ---------- */

    const renderBuildButton = (war: model.WarArtifact) => {
        const state = buildStates[war.id] || 'idle';

        if (state === 'building') {
            return (
                <button
                    className="btn btn-ghost btn-xs"
                    onClick={() => setLogModalWarId(war.id)}
                    title="View build logs"
                >
                    <span className="loading loading-spinner loading-xs" />
                </button>
            );
        }
        if (state === 'success') {
            return (
                <button
                    className="btn btn-ghost btn-xs text-success"
                    onClick={() => setLogModalWarId(war.id)}
                    title="Build succeeded — click to view logs"
                >
                    <FaCheckCircle />
                </button>
            );
        }
        if (state === 'error') {
            return (
                <button
                    className="btn btn-ghost btn-xs text-error"
                    onClick={() => setLogModalWarId(war.id)}
                    title="Build failed — click to view logs"
                >
                    <FaTimesCircle />
                </button>
            );
        }

        // idle
        const hasLogs = (buildLogs[war.id] || []).length > 0;
        return (
            <div className="flex gap-0.5 justify-center">
                <button
                    className="btn btn-ghost btn-xs"
                    onClick={() => handleBuild(war.id)}
                    title="Run Maven build"
                >
                    <FaHammer />
                </button>
                {hasLogs && (
                    <button
                        className="btn btn-ghost btn-xs text-base-content/40"
                        onClick={() => setLogModalWarId(war.id)}
                        title="View last build log"
                    >
                        <FaFileAlt />
                    </button>
                )}
            </div>
        );
    };

    /* ---------- WAR File existence indicator ---------- */

    const renderWarExistsIndicator = (warId: number) => {
        const exists = warExistsMap[warId];
        if (exists === null || exists === undefined) {
            return <span className="loading loading-spinner loading-xs" />;
        }
        if (exists) {
            return <FaCheckCircle className="text-success" />;
        }
        return <FaTimesCircle className="text-error" />;
    };

    /* ---------- Render ---------- */

    return (
        <div className="p-6 page-enter">
            {/* Header */}
            <div className="flex justify-between items-start mb-6">
                <div>
                    <h1 className="text-2xl font-bold tracking-tight">WAR Manager</h1>
                    <p className="text-sm text-base-content/40 mt-1">Manage and deploy WAR artifacts</p>
                </div>
                <div className="flex gap-2">
                    <button
                        className="btn btn-ghost btn-sm gap-2"
                        onClick={async () => {
                            setRefreshing(true);
                            await Promise.all(wars.map((w) =>
                                CheckWarExists(w.sourcePath)
                                    .then((exists) => setWarExistsMap((prev) => ({ ...prev, [w.id]: exists })))
                                    .catch(() => setWarExistsMap((prev) => ({ ...prev, [w.id]: false })))
                            ));
                            setRefreshing(false);
                        }}
                        disabled={refreshing}
                        title="Refresh WAR file status"
                    >
                        {refreshing
                            ? <span className="loading loading-spinner loading-xs" />
                            : <FaSync className="text-xs" />}
                        Refresh
                    </button>
                    <button className="btn btn-primary btn-sm gap-2" onClick={() => openModal()}>
                        <FaPlus className="text-xs" /> Add WAR
                    </button>
                    <button
                        className="btn btn-secondary btn-sm gap-2"
                        onClick={handleDeploy}
                        disabled={deploying}
                    >
                        {deploying && <span className="loading loading-spinner loading-xs" />}
                        {!deploying && <FaRocket className="text-xs" />}
                        Deploy All
                    </button>
                </div>
            </div>

            {/* Table */}
            <div className="panel overflow-hidden">
                {wars.length === 0 ? (
                    <div className="flex flex-col items-center justify-center py-16 text-base-content/30">
                        <FaBoxOpen className="text-4xl mb-3" />
                        <p className="text-sm font-medium">No WAR artifacts configured</p>
                        <p className="text-xs mt-1">Click "Add WAR" to get started</p>
                    </div>
                ) : (
                    <table className="data-table w-full">
                        <thead>
                            <tr>
                                <th className="w-20">Status</th>
                                <th>Source Path</th>
                                <th className="w-20 text-center">WAR File</th>
                                <th>Destination</th>
                                <th className="w-20 text-center">Build</th>
                                <th className="w-24 text-right">Actions</th>
                            </tr>
                        </thead>
                        <tbody>
                            {wars.map((war) => (
                                <tr key={war.id}>
                                    <td>
                                        <input
                                            type="checkbox"
                                            className="checkbox checkbox-sm checkbox-primary"
                                            checked={war.enabled}
                                            readOnly
                                        />
                                    </td>
                                    <td>
                                        <span className="font-mono text-xs text-base-content/60 truncate block max-w-md">
                                            {war.sourcePath}
                                        </span>
                                    </td>
                                    <td className="text-center">
                                        {renderWarExistsIndicator(war.id)}
                                    </td>
                                    <td>
                                        <span className="font-mono text-xs font-medium text-primary/80">
                                            {war.destName}
                                        </span>
                                    </td>
                                    <td className="text-center">
                                        {renderBuildButton(war)}
                                    </td>
                                    <td>
                                        <div className="flex gap-1 justify-end">
                                            <button
                                                className="btn btn-ghost btn-xs"
                                                onClick={() => openModal(war)}
                                                title="Edit"
                                            >
                                                <FaEdit />
                                            </button>
                                            <button
                                                className="btn btn-ghost btn-xs text-error"
                                                onClick={() => handleDelete(war.id)}
                                                title="Delete"
                                            >
                                                <FaTrash />
                                            </button>
                                        </div>
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                )}
            </div>

            {/* Add/Edit Modal */}
            {modalOpen && (
                <div className="fixed inset-0 z-50 flex items-center justify-center modal-backdrop-blur">
                    <div className="panel p-6 w-full max-w-lg mx-4">
                        <h3 className="text-lg font-bold tracking-tight mb-5">
                            {currentWar.id ? 'Edit WAR Artifact' : 'Add WAR Artifact'}
                        </h3>

                        <div className="space-y-4">
                            {/* Source Path */}
                            <div>
                                <label className="form-label">Source Path</label>
                                <div className="flex gap-2">
                                    <input
                                        type="text"
                                        className="input input-bordered w-full font-mono text-sm"
                                        placeholder="/path/to/artifact.war"
                                        value={currentWar.sourcePath}
                                        onChange={(e) => setCurrentWar({ ...currentWar, sourcePath: e.target.value })}
                                    />
                                    <button
                                        className="btn btn-square btn-sm"
                                        onClick={async () => {
                                            try {
                                                const path = await SelectWarFile();
                                                if (path) setCurrentWar((prev) => ({ ...prev, sourcePath: path }));
                                            } catch (e) { console.error(e); }
                                        }}
                                        title="Browse"
                                    >
                                        <FaFolder className="text-xs" />
                                    </button>
                                </div>
                            </div>

                            {/* Destination Name */}
                            <div>
                                <label className="form-label">Destination Name</label>
                                <input
                                    type="text"
                                    className="input input-bordered w-full font-mono text-sm"
                                    placeholder="app.war"
                                    value={currentWar.destName}
                                    onChange={(e) => setCurrentWar({ ...currentWar, destName: e.target.value })}
                                />
                            </div>

                            {/* Enabled */}
                            <label className="flex items-center gap-3 cursor-pointer">
                                <input
                                    type="checkbox"
                                    className="checkbox checkbox-sm checkbox-primary"
                                    checked={currentWar.enabled}
                                    onChange={(e) => setCurrentWar({ ...currentWar, enabled: e.target.checked })}
                                />
                                <span className="text-sm font-medium">Enabled</span>
                            </label>
                        </div>

                        {/* Actions */}
                        <div className="flex justify-end gap-2 mt-6 pt-4 border-t border-base-content/5">
                            <button className="btn btn-ghost btn-sm" onClick={() => setModalOpen(false)}>
                                Cancel
                            </button>
                            <button className="btn btn-primary btn-sm" onClick={handleSave}>
                                Save
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* Build Log Modal */}
            {logModalWarId !== null && (
                <BuildLogModal
                    warId={logModalWarId}
                    wars={wars}
                    buildStates={buildStates}
                    buildLogs={buildLogs}
                    onClose={() => setLogModalWarId(null)}
                />
            )}
        </div>
    );
};

export default WarManager;
