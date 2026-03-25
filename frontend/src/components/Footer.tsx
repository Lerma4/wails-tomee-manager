import React from 'react';

declare const __APP_VERSION__: string;

const Footer: React.FC = () => {
    return (
        <div className="px-5 py-4 border-t border-base-content/5">
            <p className="text-[0.65rem] font-mono text-base-content/25 uppercase tracking-wider">
                Apache TomEE
            </p>
            <p className="text-[0.6rem] font-mono text-base-content/20 tracking-wider mt-1">
                v{__APP_VERSION__}
            </p>
        </div>
    );
};

export default Footer;
