import { useEffect, useState } from 'react';
import { ListWars, SaveWar, DeleteWar } from '../../wailsjs/go/service/StorageService';
import { DeployAll as DeployAllWars } from '../../wailsjs/go/service/WarService';
import { SelectWarFile } from '../../wailsjs/go/main/App';
import { model } from '../../wailsjs/go/models';
import { FaPlus, FaTrash, FaEdit, FaRocket, FaFolder, FaBoxOpen } from 'react-icons/fa';

const WarManager = () => {
    const [wars, setWars] = useState<model.WarArtifact[]>([]);
    const [deploying, setDeploying] = useState(false);
    const [modalOpen, setModalOpen] = useState(false);
    const [currentWar, setCurrentWar] = useState<model.WarArtifact>(new model.WarArtifact());

    const fetchWars = () => {
        ListWars().then((data) => setWars(data || [])).catch(console.error);
    };

    useEffect(() => { fetchWars(); }, []);

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

    return (
        <div className="p-6 page-enter">
            {/* Header */}
            <div className="flex justify-between items-start mb-6">
                <div>
                    <h1 className="text-2xl font-bold tracking-tight">WAR Manager</h1>
                    <p className="text-sm text-base-content/40 mt-1">Manage and deploy WAR artifacts</p>
                </div>
                <div className="flex gap-2">
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
                                <th>Destination</th>
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
                                    <td>
                                        <span className="font-mono text-xs font-medium text-primary/80">
                                            {war.destName}
                                        </span>
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

            {/* Modal */}
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
                                                if (path) setCurrentWar({ ...currentWar, sourcePath: path });
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
        </div>
    );
};

export default WarManager;
