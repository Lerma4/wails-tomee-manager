import { useCallback, useEffect, useRef, useState } from 'react';
import { ListWars, SaveWar, DeleteWar, LoadConfig } from '../../wailsjs/go/service/StorageService';
import { DeployAll as DeployAllWars, DeploySingle, IsDeployed, Undeploy } from '../../wailsjs/go/service/WarService';
import { Restart } from '../../wailsjs/go/service/TomEEService';
import { CheckWarExists, RunBuild } from '../../wailsjs/go/service/MavenService';
import { SelectProjectDir } from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { model } from '../../wailsjs/go/models';
import { FaPlus, FaTrash, FaEdit, FaRocket, FaFolder, FaBoxOpen, FaSync, FaCheckCircle, FaTimesCircle, FaHammer, FaFileAlt, FaBolt, FaEject } from 'react-icons/fa';

type BuildState = 'idle' | 'building' | 'success' | 'error';

/** Where a deploy puts the artifact. Mirrors the constants in backend/model. */
const DEPLOY_MODES = {
    copy: {
        label: 'Copy',
        hint: 'Copies the built .war into webapps/. Tomcat unpacks it, so a rebuild needs a redeploy.',
    },
    war: {
        label: 'War',
        hint: 'Writes a context descriptor pointing at the .war in target/. Nothing is copied.',
    },
    exploded: {
        label: 'Exploded',
        hint: 'Writes a context descriptor pointing at the exploded target/ directory, so a rebuild is picked up in place.',
    },
} as const;

type DeployMode = keyof typeof DEPLOY_MODES;

const deployModeOf = (war: model.WarArtifact): DeployMode =>
    (war.deployMode in DEPLOY_MODES ? war.deployMode : 'copy') as DeployMode;

/**
 * Preview of the context Tomcat will use. contextName() in
 * backend/service/instance.go is the authority; this only mirrors it so the
 * URL can be shown while typing.
 */
const contextPreview = (value: string): string => {
    let name = value.trim().replace(/\\/g, '/');
    name = name.replace(/^\/+|\/+$/g, '');
    if (/\.war$/i.test(name)) name = name.slice(0, -4);
    name = name.replace(/^\/+|\/+$/g, '');
    if (name === '' || name.toUpperCase() === 'ROOT') return '';
    return name.replace(/\//g, '#');
};

/** The path segment to display for an artifact: what it is deployed as if it
 *  has been deployed, otherwise what the configured value will become. */
const contextUrlPath = (war: model.WarArtifact): string => {
    const name = war.deployedAs || contextPreview(war.destName || '');
    return name.toUpperCase() === 'ROOT' ? '' : name;
};

/** Stages of the Build to Deploy to Restart chain, or '' when idle. */
type ChainStage = '' | 'building' | 'deploying' | 'restarting';

const CHAIN_TIP: Record<ChainStage, string> = {
    '': 'Build, deploy, then restart TomEE',
    building: 'Building the project...',
    deploying: 'Copying the build to the server...',
    restarting: 'Restarting TomEE...',
};

// Written out rather than interpolated so the class names stay greppable.
const TOOLTIP_SIDE = {
    left: 'tooltip-left',
    right: 'tooltip-right',
    top: 'tooltip-top',
    bottom: 'tooltip-bottom',
} as const;

/**
 * Explains an icon-only control on hover.
 *
 * The wrapper matters twice over: a disabled .btn has pointer-events none and
 * would never show a tooltip of its own, and the table sits in a panel with
 * overflow-hidden — so tooltips have to point inwards or they get clipped.
 */
const Hint = ({
    tip,
    side = 'left',
    children,
}: {
    tip: string;
    side?: keyof typeof TOOLTIP_SIDE;
    children: React.ReactNode;
}) => (
    <span className={`tooltip ${TOOLTIP_SIDE[side]}`} data-tip={tip}>
        {children}
    </span>
);

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

    // biome-ignore lint/correctness/useExhaustiveDependencies: `lines` is the trigger, not a value read by the effect
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
                    <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>Close</button>
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

    // Maven profile
    const [mavenProfile, setMavenProfile] = useState('dev');

    // Only used to render the URL an artifact will answer on.
    const [httpPort, setHttpPort] = useState(8080);

    // Task 5 — WAR existence check
    const [warExistsMap, setWarExistsMap] = useState<Record<number, boolean | null>>({});
    const [deployedMap, setDeployedMap] = useState<Record<number, boolean | null>>({});

    // Build to Deploy to Restart chain. The queue is a ref because the
    // maven-done listener below is registered once per WAR and must see the
    // current value, not the one captured when it was registered.
    const chainQueue = useRef<Set<number>>(new Set());
    const [chainStage, setChainStage] = useState<Record<number, ChainStage>>({});

    // Task 6 — Maven build per-row
    const [buildStates, setBuildStates] = useState<Record<number, BuildState>>({});
    const [buildLogs, setBuildLogs] = useState<Record<number, string[]>>({});
    const [logModalWarId, setLogModalWarId] = useState<number | null>(null);
    const [refreshing, setRefreshing] = useState(false);
    const [deployingIds, setDeployingIds] = useState<Set<number>>(new Set());
    const buildTimers = useRef<Map<number, ReturnType<typeof setTimeout>>>(new Map());

    /* ---------- WAR existence helpers ---------- */

    const checkWarExists = (war: model.WarArtifact) => {
        setWarExistsMap((prev) => ({ ...prev, [war.id]: null }));
        CheckWarExists(war.sourcePath)
            .then((exists) => setWarExistsMap((prev) => ({ ...prev, [war.id]: exists })))
            .catch(() => setWarExistsMap((prev) => ({ ...prev, [war.id]: false })));
    };

    const checkDeployed = (war: model.WarArtifact) => {
        setDeployedMap((prev) => ({ ...prev, [war.id]: null }));
        IsDeployed(war.id)
            .then((deployed) => setDeployedMap((prev) => ({ ...prev, [war.id]: deployed })))
            .catch(() => setDeployedMap((prev) => ({ ...prev, [war.id]: false })));
    };

    const checkAllWarExists = (warList: model.WarArtifact[]) => {
        warList.forEach((w) => {
            checkWarExists(w);
            checkDeployed(w);
        });
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

    // biome-ignore lint/correctness/useExhaustiveDependencies: initial load only; fetchWars is re-created every render
    useEffect(() => { fetchWars(); }, []);

    useEffect(() => {
        LoadConfig()
            .then((cfg) => { if (cfg.httpPort) setHttpPort(cfg.httpPort); })
            .catch(console.error);
    }, []);

    /* ---------- Build to Deploy to Restart chain ---------- */

    // Only stable references are used here: the listener registered per WAR
    // keeps whichever closure it was created with.
    const runDeployAndRestart = useCallback(async (warId: number) => {
        try {
            setChainStage((prev) => ({ ...prev, [warId]: 'deploying' }));
            await DeploySingle(warId);
            setChainStage((prev) => ({ ...prev, [warId]: 'restarting' }));
            await Restart();
        } catch (err) {
            alert(`Build and Run failed: ${err}`);
        } finally {
            setChainStage((prev) => ({ ...prev, [warId]: '' }));
            IsDeployed(warId)
                .then((deployed) => setDeployedMap((prev) => ({ ...prev, [warId]: deployed })))
                .catch(() => undefined);
        }
    }, []);

    /* ---------- Event listeners for Maven build ---------- */

    const warsRef = useRef<model.WarArtifact[]>([]);
    useEffect(() => { warsRef.current = wars; }, [wars]);

    const activeListeners = useRef<Map<number, () => void>>(new Map());

    // Incremental listener management: register new, unregister removed
    // biome-ignore lint/correctness/useExhaustiveDependencies: keyed on `wars` only — see 46cf3d7, adding deps tears listeners down mid-build
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

                // Continue the chain only when the build actually produced something.
                if (chainQueue.current.delete(war.id)) {
                    if (result.success) {
                        runDeployAndRestart(war.id);
                    } else {
                        setChainStage((prev) => ({ ...prev, [war.id]: '' }));
                    }
                }

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
            activeListeners.current.forEach((cleanup) => { cleanup(); });
            activeListeners.current.clear();
            buildTimers.current.forEach((timer) => { clearTimeout(timer); });
            buildTimers.current.clear();
        };
    }, []);

    /* ---------- Build handler ---------- */

    const handleBuild = async (warId: number) => {
        setBuildStates((prev) => ({ ...prev, [warId]: 'building' }));
        setBuildLogs((prev) => ({ ...prev, [warId]: [] }));
        try {
            await RunBuild(warId, mavenProfile);
        } catch (err) {
            // No maven-done event will arrive, so the chain has to be released here.
            chainQueue.current.delete(warId);
            setChainStage((prev) => ({ ...prev, [warId]: '' }));
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

    const handleBuildAndRun = async (warId: number) => {
        chainQueue.current.add(warId);
        setChainStage((prev) => ({ ...prev, [warId]: 'building' }));
        await handleBuild(warId);
    };

    const handleUndeploy = async (warId: number) => {
        if (!window.confirm('Remove this artifact from the server? The build output is left untouched.')) return;
        try {
            await Undeploy(warId);
        } catch (err) {
            alert(`Undeploy failed: ${err}`);
        } finally {
            const war = warsRef.current.find((w) => w.id === warId);
            if (war) checkDeployed(war);
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
            alert(`Error saving WAR: ${err}`);
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
            warsRef.current.forEach((w) => { checkDeployed(w); });
            alert('Deployment successful!');
        } catch (err) {
            alert(`Deployment failed: ${err}`);
        } finally {
            setDeploying(false);
        }
    };

    const handleDeploySingle = async (warId: number) => {
        setDeployingIds((prev) => new Set(prev).add(warId));
        try {
            await DeploySingle(warId);
        } catch (err) {
            alert(`Deploy failed: ${err}`);
        } finally {
            setDeployingIds((prev) => {
                const next = new Set(prev);
                next.delete(warId);
                return next;
            });
            const war = warsRef.current.find((w) => w.id === warId);
            if (war) checkDeployed(war);
        }
    };

    const openModal = (war?: model.WarArtifact) => {
        setCurrentWar(
            war
                ? { ...war, deployMode: deployModeOf(war) }
                : model.WarArtifact.createFrom({ enabled: true, deployMode: 'copy' }),
        );
        setModalOpen(true);
    };

    /* ---------- Build column button ---------- */

    const renderBuildButton = (war: model.WarArtifact) => {
        const state = buildStates[war.id] || 'idle';

        if (state === 'building') {
            return (
                <Hint tip="Build running — click to follow the output">
                    <button type="button"
                        className="btn btn-ghost btn-xs"
                        onClick={() => setLogModalWarId(war.id)}
                    >
                        <span className="loading loading-spinner loading-xs" />
                    </button>
                </Hint>
            );
        }
        if (state === 'success') {
            return (
                <Hint tip="Build succeeded — click to see the output">
                    <button type="button"
                        className="btn btn-ghost btn-xs text-success"
                        onClick={() => setLogModalWarId(war.id)}
                    >
                        <FaCheckCircle />
                    </button>
                </Hint>
            );
        }
        if (state === 'error') {
            return (
                <Hint tip="Build failed — click to see why">
                    <button type="button"
                        className="btn btn-ghost btn-xs text-error"
                        onClick={() => setLogModalWarId(war.id)}
                    >
                        <FaTimesCircle />
                    </button>
                </Hint>
            );
        }

        // idle
        const hasLogs = (buildLogs[war.id] || []).length > 0;
        return (
            <div className="flex gap-0.5 justify-center">
                <Hint tip={`Run mvn install -DskipTests -P${mavenProfile || 'dev'} in this project`}>
                    <button type="button"
                        className="btn btn-ghost btn-xs"
                        onClick={() => handleBuild(war.id)}
                    >
                        <FaHammer />
                    </button>
                </Hint>
                {hasLogs && (
                    <Hint tip="Show the output of the last build">
                        <button type="button"
                            className="btn btn-ghost btn-xs text-base-content/40"
                            onClick={() => setLogModalWarId(war.id)}
                        >
                            <FaFileAlt />
                        </button>
                    </Hint>
                )}
            </div>
        );
    };

    /* ---------- WAR File existence indicator ---------- */

    const renderWarExistsIndicator = (warId: number) => {
        const exists = warExistsMap[warId];
        if (exists === null || exists === undefined) {
            return (
                <Hint tip="Looking for a .war in target/..." side="right">
                    <span className="loading loading-spinner loading-xs" />
                </Hint>
            );
        }
        if (exists) {
            return (
                <Hint tip="A built .war is present in target/" side="right">
                    <FaCheckCircle className="text-success" />
                </Hint>
            );
        }
        return (
            <Hint tip="No .war in target/ — build the project first" side="right">
                <FaTimesCircle className="text-error" />
            </Hint>
        );
    };

    /* ---------- Deployed indicator ---------- */

    const renderDeployedIndicator = (warId: number) => {
        if (deployedMap[warId] !== true) return null;
        return (
            <Hint tip="This artifact is on the server right now">
                <span className="badge badge-xs badge-success ml-2 align-middle">on server</span>
            </Hint>
        );
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
                    <Hint tip="Re-check which projects have a build in target/, and what is on the server" side="bottom">
                    <button type="button"
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
                    >
                        {refreshing
                            ? <span className="loading loading-spinner loading-xs" />
                            : <FaSync className="text-xs" />}
                        Refresh
                    </button>
                    </Hint>
                    <div className="flex items-center gap-1">
                        <label className="text-xs text-base-content/40" htmlFor="maven-profile">Profile:</label>
                        <Hint tip="Maven profile for every build, passed as -P<name>" side="bottom">
                        <input
                            id="maven-profile"
                            type="text"
                            className="input input-bordered input-sm font-mono text-xs w-24"
                            value={mavenProfile}
                            onChange={(e) => setMavenProfile(e.target.value)}
                            placeholder="dev"
                        />
                        </Hint>
                    </div>
                    <Hint tip="Add a Maven project to this list" side="bottom">
                        <button type="button" className="btn btn-primary btn-sm gap-2" onClick={() => openModal()}>
                            <FaPlus className="text-xs" /> Add WAR
                        </button>
                    </Hint>
                    <Hint tip="Deploy every enabled artifact, without rebuilding" side="bottom">
                    <button type="button"
                        className="btn btn-secondary btn-sm gap-2"
                        onClick={handleDeploy}
                        disabled={deploying}
                    >
                        {deploying && <span className="loading loading-spinner loading-xs" />}
                        {!deploying && <FaRocket className="text-xs" />}
                        Deploy All
                    </button>
                    </Hint>
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
                                <th className="w-20">
                                    <Hint tip="Enabled artifacts are the ones Deploy All touches" side="right">
                                        <span>Status</span>
                                    </Hint>
                                </th>
                                <th>Project Path</th>
                                <th className="w-20 text-center">
                                    <Hint tip="Whether the project has a built .war in target/" side="right">
                                        <span>WAR File</span>
                                    </Hint>
                                </th>
                                <th>
                                    <Hint tip="The URL path the app answers on — independent of the .war file name" side="right">
                                        <span>Context</span>
                                    </Hint>
                                </th>
                                <th className="w-24 text-center">Mode</th>
                                <th className="w-20 text-center">Build</th>
                                <th className="w-32 text-right">Actions</th>
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
                                            /{contextUrlPath(war)}
                                        </span>
                                        {renderDeployedIndicator(war.id)}
                                    </td>
                                    <td className="text-center">
                                        <Hint tip={DEPLOY_MODES[deployModeOf(war)].hint}>
                                            <span className="badge badge-sm badge-ghost font-mono text-[0.65rem]">
                                                {DEPLOY_MODES[deployModeOf(war)].label}
                                            </span>
                                        </Hint>
                                    </td>
                                    <td className="text-center">
                                        {renderBuildButton(war)}
                                    </td>
                                    <td>
                                        <div className="flex gap-1 justify-end">
                                            <Hint tip={CHAIN_TIP[chainStage[war.id] || '']}>
                                                <button type="button"
                                                    className="btn btn-ghost btn-xs text-primary"
                                                    onClick={() => handleBuildAndRun(war.id)}
                                                    disabled={(chainStage[war.id] || '') !== ''}
                                                >
                                                    {chainStage[war.id]
                                                        ? <span className="loading loading-spinner loading-xs" />
                                                        : <FaBolt />}
                                                </button>
                                            </Hint>
                                            <Hint tip="Deploy the existing build to the server, without rebuilding">
                                                <button type="button"
                                                    className="btn btn-ghost btn-xs text-secondary"
                                                    onClick={() => handleDeploySingle(war.id)}
                                                    disabled={deployingIds.has(war.id)}
                                                >
                                                    {deployingIds.has(war.id)
                                                        ? <span className="loading loading-spinner loading-xs" />
                                                        : <FaRocket />}
                                                </button>
                                            </Hint>
                                            <Hint tip="Remove from the server — the build in target/ is kept. Stop TomEE first.">
                                                <button type="button"
                                                    className="btn btn-ghost btn-xs text-warning"
                                                    onClick={() => handleUndeploy(war.id)}
                                                    disabled={deployedMap[war.id] === false}
                                                >
                                                    <FaEject />
                                                </button>
                                            </Hint>
                                            <Hint tip="Edit the project path, deployment name and deploy mode">
                                                <button type="button"
                                                    className="btn btn-ghost btn-xs"
                                                    onClick={() => openModal(war)}
                                                >
                                                    <FaEdit />
                                                </button>
                                            </Hint>
                                            <Hint tip="Remove from this list — does not undeploy it from the server">
                                                <button type="button"
                                                    className="btn btn-ghost btn-xs text-error"
                                                    onClick={() => handleDelete(war.id)}
                                                >
                                                    <FaTrash />
                                                </button>
                                            </Hint>
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
                            {/* Project Path */}
                            <div>
                                <label className="form-label" htmlFor="war-source-path">Project Path</label>
                                <div className="flex gap-2">
                                    <input
                                        type="text"
                                        className="input input-bordered w-full font-mono text-sm"
                                        id="war-source-path"
                                        placeholder="/path/to/maven/project"
                                        value={currentWar.sourcePath}
                                        onChange={(e) => setCurrentWar({ ...currentWar, sourcePath: e.target.value })}
                                    />
                                    <button type="button"
                                        className="btn btn-square btn-sm"
                                        onClick={async () => {
                                            try {
                                                const path = await SelectProjectDir();
                                                if (path) setCurrentWar((prev) => ({ ...prev, sourcePath: path }));
                                            } catch (e) { console.error(e); }
                                        }}
                                        title="Browse"
                                    >
                                        <FaFolder className="text-xs" />
                                    </button>
                                </div>
                            </div>

                            {/* Context path */}
                            <div>
                                <label className="form-label" htmlFor="war-dest-name">Context path</label>
                                <input
                                    id="war-dest-name"
                                    type="text"
                                    className="input input-bordered w-full font-mono text-sm"
                                    placeholder="/commerciale"
                                    value={currentWar.destName}
                                    onChange={(e) => setCurrentWar({ ...currentWar, destName: e.target.value })}
                                />
                                <p className="text-xs text-base-content/40 mt-1">
                                    Served at{' '}
                                    <span className="font-mono text-base-content/70">
                                        http://localhost:{httpPort}/{contextPreview(currentWar.destName || '')}
                                    </span>
                                    . Use <span className="font-mono">/</span> for the root context.
                                </p>
                                <p className="text-xs text-base-content/40 mt-1">
                                    {deployModeOf(currentWar) === 'copy'
                                        ? 'In Copy mode Tomcat takes the context from the file name, so the copy in webapps/ is renamed to match this path.'
                                        : 'The built .war keeps its own name — only the context descriptor is named after this path.'}
                                </p>
                            </div>

                            {/* Deploy Mode */}
                            <div>
                                <label className="form-label" htmlFor="war-deploy-mode">Deploy Mode</label>
                                <select
                                    id="war-deploy-mode"
                                    className="select select-bordered w-full text-sm"
                                    value={deployModeOf(currentWar)}
                                    onChange={(e) => setCurrentWar({ ...currentWar, deployMode: e.target.value })}
                                >
                                    {Object.entries(DEPLOY_MODES).map(([value, mode]) => (
                                        <option key={value} value={value}>{mode.label}</option>
                                    ))}
                                </select>
                                <p className="text-xs text-base-content/40 mt-1">
                                    {DEPLOY_MODES[deployModeOf(currentWar)].hint}
                                </p>
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
                            <button type="button" className="btn btn-ghost btn-sm" onClick={() => setModalOpen(false)}>
                                Cancel
                            </button>
                            <button type="button" className="btn btn-primary btn-sm" onClick={handleSave}>
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
