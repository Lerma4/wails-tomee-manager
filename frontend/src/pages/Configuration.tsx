import { useEffect, useState } from 'react';
import { LoadConfig, SaveConfig } from '../../wailsjs/go/service/StorageService';
import { SelectDirectory } from '../../wailsjs/go/main/App';
import { model } from '../../wailsjs/go/models';
import { FaFolder, FaSave } from 'react-icons/fa';

const Configuration = () => {
    const [config, setConfig] = useState<model.Config>(new model.Config());
    const [loading, setLoading] = useState(false);
    const [message, setMessage] = useState('');

    useEffect(() => {
        LoadConfig().then(setConfig).catch(console.error);
    }, []);

    const handleSave = async () => {
        setLoading(true);
        try {
            const cfg = { ...config };
            cfg.httpPort = parseInt(cfg.httpPort as any);
            cfg.debugPort = parseInt(cfg.debugPort as any);
            cfg.shutdownPort = parseInt(cfg.shutdownPort as any);

            await SaveConfig(cfg);
            setMessage('Configuration saved successfully!');
            setTimeout(() => setMessage(''), 3000);
        } catch (err: any) {
            setMessage('Error saving config: ' + err);
        } finally {
            setLoading(false);
        }
    };

    const DirectoryField = ({ label, placeholder, value, onChange }: {
        label: string; placeholder: string; value: string;
        onChange: (val: string) => void;
    }) => (
        <div>
            <label className="form-label">{label}</label>
            <div className="flex gap-2">
                <input
                    type="text"
                    placeholder={placeholder}
                    className="input input-bordered w-full font-mono text-sm"
                    value={value}
                    onChange={(e) => onChange(e.target.value)}
                />
                <button
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
                                <label className="form-label">HTTP Port</label>
                                <input
                                    type="number"
                                    className="input input-bordered w-full font-mono text-sm"
                                    value={config.httpPort}
                                    onChange={(e) => setConfig({ ...config, httpPort: parseInt(e.target.value) })}
                                />
                            </div>
                            <div>
                                <label className="form-label">Debug Port</label>
                                <input
                                    type="number"
                                    className="input input-bordered w-full font-mono text-sm"
                                    value={config.debugPort}
                                    onChange={(e) => setConfig({ ...config, debugPort: parseInt(e.target.value) })}
                                />
                            </div>
                            <div>
                                <label className="form-label">Shutdown Port</label>
                                <input
                                    type="number"
                                    className="input input-bordered w-full font-mono text-sm"
                                    value={config.shutdownPort}
                                    onChange={(e) => setConfig({ ...config, shutdownPort: parseInt(e.target.value) })}
                                />
                            </div>
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
                        <button
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
