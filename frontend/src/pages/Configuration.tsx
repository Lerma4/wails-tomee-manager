import React, { useEffect, useState } from 'react';
import { LoadConfig, SaveConfig } from '../../wailsjs/go/service/StorageService';
import { SelectDirectory } from '../../wailsjs/go/main/App';
import { model } from '../../wailsjs/go/models';

const Configuration: React.FC = () => {
    const [config, setConfig] = useState<model.Config>(new model.Config());
    const [loading, setLoading] = useState(false);
    const [message, setMessage] = useState('');

    useEffect(() => {
        LoadConfig().then(setConfig).catch(console.error);
    }, []);

    const handleSave = async () => {
        setLoading(true);
        try {
            // Ensure ports are integers
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

    return (
        <div className="p-6">
            <h1 className="text-3xl font-bold mb-6">Configuration</h1>
            
            <div className="card bg-base-100 shadow-xl max-w-2xl">
                <div className="card-body">
                    <div className="form-control w-full">
                        <label className="label">
                            <span className="label-text">TomEE Path</span>
                        </label>
                        <div className="flex gap-2">
                            <input 
                                type="text" 
                                placeholder="C:\path\to\tomee" 
                                className="input input-bordered w-full" 
                                value={config.tomeePath}
                                onChange={(e) => setConfig({...config, tomeePath: e.target.value})}
                            />
                            <button className="btn btn-square" onClick={async () => {
                                try {
                                    const path = await SelectDirectory();
                                    if (path) setConfig({...config, tomeePath: path});
                                } catch (e) {
                                    console.error(e);
                                }
                            }}>
                                <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="w-6 h-6">
                                    <path strokeLinecap="round" strokeLinejoin="round" d="M2.25 12.75V12A2.25 2.25 0 014.5 9.75h15A2.25 2.25 0 0121.75 12v.75m-8.69-6.44l-2.12-2.12a1.5 1.5 0 00-1.061-.44H4.5A2.25 2.25 0 002.25 6v12a2.25 2.25 0 002.25 2.25h15A2.25 2.25 0 0021.75 18V9a2.25 2.25 0 00-2.25-2.25h-5.379a1.5 1.5 0 01-1.06-.44z" />
                                </svg>
                            </button>
                        </div>
                    </div>

                    <div className="grid grid-cols-3 gap-4 mt-4">
                        <div className="form-control w-full">
                            <label className="label">
                                <span className="label-text">HTTP Port</span>
                            </label>
                            <input 
                                type="number" 
                                className="input input-bordered w-full" 
                                value={config.httpPort}
                                onChange={(e) => setConfig({...config, httpPort: parseInt(e.target.value)})}
                            />
                        </div>
                        <div className="form-control w-full">
                            <label className="label">
                                <span className="label-text">Debug Port</span>
                            </label>
                            <input 
                                type="number" 
                                className="input input-bordered w-full" 
                                value={config.debugPort}
                                onChange={(e) => setConfig({...config, debugPort: parseInt(e.target.value)})}
                            />
                        </div>
                        <div className="form-control w-full">
                            <label className="label">
                                <span className="label-text">Shutdown Port</span>
                            </label>
                            <input 
                                type="number" 
                                className="input input-bordered w-full" 
                                value={config.shutdownPort}
                                onChange={(e) => setConfig({...config, shutdownPort: parseInt(e.target.value)})}
                            />
                        </div>
                    </div>

                    <div className="card-actions justify-end mt-6">
                        <button 
                            className={`btn btn-primary ${loading ? 'loading' : ''}`} 
                            onClick={handleSave}
                        >
                            Save Configuration
                        </button>
                    </div>
                    
                    {message && (
                        <div className={`alert ${message.includes('Error') ? 'alert-error' : 'alert-success'} mt-4`}>
                            <span>{message}</span>
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
};

export default Configuration;
