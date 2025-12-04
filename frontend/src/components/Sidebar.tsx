import React from 'react';
import { Link, useLocation } from 'react-router-dom';
import { FaServer, FaCog, FaBoxOpen } from 'react-icons/fa';

const Sidebar: React.FC = () => {
    const location = useLocation();

    const isActive = (path: string) => location.pathname === path ? 'active' : '';

    return (
        <div className="h-full bg-base-200 text-base-content w-64 flex flex-col shrink-0">
                <div className="p-4 text-2xl font-bold text-primary flex items-center gap-2">
                    {/* @ts-ignore */}
                    <FaServer /> TomEE Mgr
                </div>
                <ul className="menu p-4 w-64 bg-base-200 text-base-content">
                    <li>
                        <Link to="/" className={isActive('/')}>
                            {/* @ts-ignore */}
                            <FaServer /> Dashboard
                        </Link>
                    </li>
                    <li>
                        <Link to="/wars" className={isActive('/wars')}>
                            {/* @ts-ignore */}
                            <FaBoxOpen /> WAR Manager
                        </Link>
                    </li>
                    <li>
                        <Link to="/config" className={isActive('/config')}>
                            {/* @ts-ignore */}
                            <FaCog /> Configuration
                        </Link>
                    </li>
                </ul>
        </div>
    );
};

export default Sidebar;
