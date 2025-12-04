import React, { useEffect, useState } from 'react';
import { ListWars, SaveConfig, SaveWar, DeleteWar } from '../../wailsjs/go/service/StorageService';
import { DeployAll as DeployAllWars } from '../../wailsjs/go/service/WarService';
import { SelectWarFile } from '../../wailsjs/go/main/App';
import { model } from '../../wailsjs/go/models';
import { FaPlus, FaTrash, FaEdit, FaPlay } from 'react-icons/fa';

const WarManager: React.FC = () => {
    const [wars, setWars] = useState<model.WarArtifact[]>([]);
    const [loading, setLoading] = useState(false);
    const [deploying, setDeploying] = useState(false);
    const [modalOpen, setModalOpen] = useState(false);
    const [currentWar, setCurrentWar] = useState<model.WarArtifact>(new model.WarArtifact());

    const fetchWars = () => {
        ListWars().then((data) => setWars(data || [])).catch(console.error);
    };

    useEffect(() => {
        fetchWars();
    }, []);

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
        if (!confirm('Are you sure?')) return;
        try {
            await DeleteWar(id);
            fetchWars();
        } catch (err) {
            console.error(err);
        }
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

    return (
        <div className="p-6">
            <div className="flex justify-between items-center mb-6">
                <h1 className="text-3xl font-bold">WAR Manager</h1>
                <div className="flex gap-2">
                    <button className="btn btn-primary" onClick={() => openModal()}>
                        {/* @ts-ignore */}
                        <FaPlus /> Add WAR
                    </button>
                    <button 
                        className={`btn btn-secondary ${deploying ? 'loading' : ''}`} 
                        onClick={handleDeploy}
                    >
                        {/* @ts-ignore */}
                        <FaPlay /> Deploy All
                    </button>
                </div>
            </div>

            <div className="overflow-x-auto">
                <table className="table w-full">
                    <thead>
                        <tr>
                            <th>Enabled</th>
                            <th>Source Path</th>
                            <th>Destination Name</th>
                            <th>Actions</th>
                        </tr>
                    </thead>
                    <tbody>
                        {wars.map((war) => (
                            <tr key={war.id}>
                                <td>
                                    <input 
                                        type="checkbox" 
                                        className="checkbox" 
                                        checked={war.enabled} 
                                        readOnly 
                                    />
                                </td>
                                <td>{war.sourcePath}</td>
                                <td>{war.destName}</td>
                                <td className="flex gap-2">
                                    <button className="btn btn-sm btn-ghost" onClick={() => openModal(war)}>
                                        {/* @ts-ignore */}
                                        <FaEdit />
                                    </button>
                                    <button className="btn btn-sm btn-ghost text-error" onClick={() => handleDelete(war.id)}>
                                        {/* @ts-ignore */}
                                        <FaTrash />
                                    </button>
                                </td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>

            {/* Modal */}
            {modalOpen && (
                <div className="modal modal-open">
                    <div className="modal-box">
                        <h3 className="font-bold text-lg">{currentWar.id ? 'Edit WAR' : 'Add WAR'}</h3>
                        
                        <div className="form-control w-full mt-4">
                            <label className="label">
                                <span className="label-text">Source Path</span>
                            </label>
                            <div className="flex gap-2">
                                <input 
                                    type="text" 
                                    className="input input-bordered w-full" 
                                    value={currentWar.sourcePath}
                                    onChange={(e) => setCurrentWar({...currentWar, sourcePath: e.target.value})}
                                />
                                <button className="btn btn-square" onClick={async () => {
                                    try {
                                        const path = await SelectWarFile();
                                        if (path) setCurrentWar({...currentWar, sourcePath: path});
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

                        <div className="form-control w-full mt-4">
                            <label className="label">
                                <span className="label-text">Destination Name (e.g. app.war)</span>
                            </label>
                            <input 
                                type="text" 
                                className="input input-bordered w-full" 
                                value={currentWar.destName}
                                onChange={(e) => setCurrentWar({...currentWar, destName: e.target.value})}
                            />
                        </div>

                        <div className="form-control w-full mt-4">
                            <label className="label cursor-pointer">
                                <span className="label-text">Enabled</span>
                                <input 
                                    type="checkbox" 
                                    className="checkbox" 
                                    checked={currentWar.enabled}
                                    onChange={(e) => setCurrentWar({...currentWar, enabled: e.target.checked})}
                                />
                            </label>
                        </div>

                        <div className="modal-action">
                            <button className="btn" onClick={() => setModalOpen(false)}>Cancel</button>
                            <button className="btn btn-primary" onClick={handleSave}>Save</button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
};

export default WarManager;
