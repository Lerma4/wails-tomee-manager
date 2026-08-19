import { useCallback, useEffect, useId, useState } from 'react';
import { LoadConfig, SaveConfig } from '../../wailsjs/go/service/StorageService';
import { InstanceDir, ResetInstance } from '../../wailsjs/go/service/TomEEService';
import { SelectDirectory } from '../../wailsjs/go/main/App';
import { model } from '../../wailsjs/go/models';
import { FaFolder, FaSave, FaUndo } from 'react-icons/fa';

const Configuration = () => {
    const [config, setConfig] = useState<model.Config>(new model.Config());
    const [loading, setLoading] = useState(false);
    const [message, setMessage] = useState('');
    const [instanceDir, setInstanceDir] = useState('');

    // Stable so the mount effect below can depend on it honestly.
    const refreshInstanceDir = useCallback(() => {
        InstanceDir()
            .then(setInstanceDir)
            .catch(() => setInstanceDir(''));
    }, []);

    useEffect(() => {
        LoadConfig().then(setConfig).catch(console.error);
        refreshInstanceDir();
    }, [refreshInstanceDir]);

    const handleResetInstance = async () => {
        const warning =
            'Delete the isolated instance directory and seed it again from the TomEE installation? Anything deployed there is removed.';
        if (!window.confirm(warning)) return;
        try {
            await ResetInstance();
            setMessage('Instance directory reset.');
            setTimeout(() => setMessage(''), 3000);
        } catch (err) {
            setMessage(`Error resetting instance: ${err}`);
        }
    };

    const handleSave = async () => {
        setLoading(true);
        try {
            const cfg = { ...config };
            cfg.httpPort = Number(cfg.httpPort);
            cfg.debugPort = Number(cfg.debugPort);
            cfg.shutdownPort = Number(cfg.shutdownPort);

            await SaveConfig(cfg);
            refreshInstanceDir();
            setMessage('Configuration saved successfully!');
            setTimeout(() => setMessage(''), 3000);
        } catch (err) {
            setMessage(`Error saving config: ${err}`);
        } finally {
            setLoading(false);
        }
    };

    const DirectoryField = ({ label, placeholder, value, onChange }: {
        label: string; placeholder: string; value: string;
        onChange: (val: string) => void;
    }) => {
        const fieldId = useId();
        return (
        <div>
            <label className="form-label" htmlFor={fieldId}>{label}</label>
            <div className="flex gap-2">
                <input
                    id={fieldId}
                    type="text"
                    placeholder={placeholder}
                    className="input input-bordered w-full font-mono text-sm"
                    value={value}
                    onChange={(e) => onChange(e.target.value)}
                />
                <button type="button"
                    className="btn btn-square btn-sm"
                    onClick={async () => {
                        try {
                            const path = await SelectDirectory();
                            if (path) onChange(path);
                        } catch (e) { console.error(e); }
                    }}
                    title="Browse"
                >
<FaFolder className="text-xs" />
                </button>
            </div>
        </div>
        );
    };

    return (
        <div className="p-6 page-enter">
            {/* Header */}
            <div className="mb-6">
                <h1 className="text-2xl font-bold tracking-tight">Configuration</h1>
                <p className="text-sm text-base-content/40 mt-1">Configure TomEE server paths and ports</p>
            </div>

            <div className="panel p-6 max-w-2xl">
                <div className="space-y-5">
                    {/* Paths Section */}
                    <div>
                        <h2 className="text-xs font-bold uppercase tracking-widest text-base-content/30 mb-4">
                            Paths
                        </h2>
                        <div className="space-y-4">
                            <DirectoryField
                                label="TomEE Home"
                                placeholder="C:\path\to\tomee"
                                value={config.tomeePath}
                                onChange={(val) => setConfig({ ...config, tomeePath: val })}
                            />
                            <DirectoryField
                                label="JAVA_HOME (leave empty for system default)"
                                placeholder="C:\path\to\jdk"
                                value={config.javaHome}
                                onChange={(val) => setConfig({ ...config, javaHome: val })}
                            />
                        </div>
                    </div>

                    {/* Divider */}
                    <div className="border-t border-base-content/5" />

                    {/* Ports Section */}
                    <div>
                        <h2 className="text-xs font-bold uppercase tracking-widest text-base-content/30 mb-4">
                            Ports
                        </h2>
                        <div className="grid grid-cols-3 gap-4">
                            <div>
                                <label className="form-label" htmlFor="http-port">HTTP Port</label>
                                <input
                                    id="http-port"
                                    type="number"
                                    className="input input-bordered w-full font-mono text-sm"
                                    value={config.httpPort}
                                    onChange={(e) => setConfig({ ...config, httpPort: parseInt(e.target.value, 10) })}
                                />
                            </div>
                            <div>
                                <label className="form-label" htmlFor="debug-port">Debug Port</label>
                                <input
                                    id="debug-port"
                                    type="number"
                                    className="input input-bordered w-full font-mono text-sm"
                                    value={config.debugPort}
                                    onChange={(e) => setConfig({ ...config, debugPort: parseInt(e.target.value, 10) })}
                                />
                            </div>
                            <div>
                                <label className="form-label" htmlFor="shutdown-port">Shutdown Port</label>
                                <input
                                    id="shutdown-port"
                                    type="number"
                                    className="input input-bordered w-full font-mono text-sm"
                                    value={config.shutdownPort}
                                    onChange={(e) => setConfig({ ...config, shutdownPort: parseInt(e.target.value, 10) })}
                                />
                            </div>
                        </div>
                    </div>

                    {/* Divider */}
                    <div className="border-t border-base-content/5" />

                    {/* Runtime Section */}
                    <div>
                        <h2 className="text-xs font-bold uppercase tracking-widest text-base-content/30 mb-4">
                            Runtime
                        </h2>
                        <div className="space-y-4">
                            <div>
                                <label className="form-label" htmlFor="vm-options">
                                    VM Options (passed as CATALINA_OPTS)
                                </label>
                                <input
                                    id="vm-options"
                                    type="text"
                                    className="input input-bordered w-full font-mono text-sm"
                                    placeholder="-Xmx2g -XX:MaxMetaspaceSize=512m -Dkey=value"
                                    value={config.vmOptions ?? ''}
                                    onChange={(e) => setConfig({ ...config, vmOptions: e.target.value })}
                                />
                            </div>
                            <label className="flex items-start gap-3 cursor-pointer" htmlFor="open-browser">
                                <input
                                    id="open-browser"
                                    type="checkbox"
                                    className="checkbox checkbox-sm mt-0.5"
                                    checked={config.openBrowser ?? false}
                                    onChange={(e) => setConfig({ ...config, openBrowser: e.target.checked })}
                                />
                                <span className="text-sm">
                                    Open the browser on startup
                                    <span className="block text-xs text-base-content/40">
                                        Fires when the server logs "Server startup in ...", not when the process starts.
                                    </span>
                                </span>
                            </label>
                        </div>
                    </div>

                    {/* Divider */}
                    <div className="border-t border-base-content/5" />

                    {/* Instance Section */}
                    <div>
                        <h2 className="text-xs font-bold uppercase tracking-widest text-base-content/30 mb-4">
                            Instance
                        </h2>
                        <label className="flex items-start gap-3 cursor-pointer" htmlFor="isolated-base">
                            <input
                                id="isolated-base"
                                type="checkbox"
                                className="checkbox checkbox-sm mt-0.5"
                                checked={config.isolatedBase ?? false}
                                onChange={(e) => setConfig({ ...config, isolatedBase: e.target.checked })}
                            />
                            <span className="text-sm">
                                Run against an isolated instance directory
                                <span className="block text-xs text-base-content/40">
                                    Uses a private CATALINA_BASE seeded from the conf/ of the installation, so server.xml
                                    and webapps/ inside the TomEE install are never modified. Turning this on gives the
                                    server an empty webapps directory: deploy again afterwards.
                                </span>
                            </span>
                        </label>
                        <div className="mt-3 flex items-end gap-2">
                            <div className="flex-1">
                                <span className="form-label">CATALINA_BASE in use</span>
                                <input
                                    type="text"
                                    readOnly
                                    className="input input-bordered w-full font-mono text-xs opacity-70"
                                    value={instanceDir || 'not configured'}
                                />
                            </div>
                            <button
                                type="button"
                                className="btn btn-sm btn-outline gap-2"
                                onClick={handleResetInstance}
                                disabled={!config.isolatedBase}
                                title="Delete and seed the isolated instance directory again"
                            >
                                <FaUndo className="text-xs" />
                                Reset
                            </button>
                        </div>
                    </div>

                    {/* Divider */}
                    <div className="border-t border-base-content/5" />

                    {/* Save */}
                    <div className="flex items-center justify-between">
                        <div>
                            {message && (
                                <span className={`text-sm font-medium ${
                                    message.includes('Error') ? 'text-error' : 'text-success'
                                }`}>
                                    {message}
                                </span>
                            )}
                        </div>
                        <button type="button"
                            className="btn btn-primary btn-sm gap-2"
                            onClick={handleSave}
                            disabled={loading}
                        >
                            {loading && <span className="loading loading-spinner loading-xs" />}
                {!loading && <FaSave className="text-xs" />}
                            Save Configuration
                        </button>
                    </div>
                </div>
            </div>
        </div>
    );
};

export default Configuration;
