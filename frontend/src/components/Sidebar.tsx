import React from 'react';
import { Link, useLocation } from 'react-router-dom';
import { FaServer, FaCog, FaBoxOpen } from 'react-icons/fa';
import Footer from './Footer';

const navItems = [
    { path: '/', label: 'Dashboard', icon: FaServer },
    { path: '/wars', label: 'WAR Manager', icon: FaBoxOpen },
    { path: '/config', label: 'Configuration', icon: FaCog },
];

const Sidebar: React.FC = () => {
    const location = useLocation();

    return (
        <aside className="sidebar-nav h-full w-60 shrink-0 flex flex-col bg-base-200">
            {/* Brand */}
            <div className="px-5 py-5 flex items-center gap-3">
                <div className="w-8 h-8 rounded-lg bg-primary/15 flex items-center justify-center">
                    {/* @ts-ignore */}
                    <FaServer className="text-primary text-sm" />
                </div>
                <div>
                    <h1 className="text-sm font-bold tracking-wide text-base-content">TomEE Manager</h1>
                    <span className="text-[0.65rem] font-mono text-base-content/40 uppercase tracking-widest">ops console</span>
                </div>
            </div>

            {/* Divider */}
            <div className="mx-4 border-t border-base-content/5" />

            {/* Navigation */}
            <nav className="flex-1 px-3 py-4">
                <ul className="menu gap-1 p-0">
                    {navItems.map(({ path, label, icon: Icon }) => (
                        <li key={path}>
                            <Link
                                to={path}
                                className={location.pathname === path ? 'active' : ''}
                            >
                                {/* @ts-ignore */}
                                <Icon className="text-[0.9rem]" />
                                {label}
                            </Link>
                        </li>
                    ))}
                </ul>
            </nav>

            <Footer />
        </aside>
    );
};

export default Sidebar;
