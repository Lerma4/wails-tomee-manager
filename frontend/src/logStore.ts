import { useSyncExternalStore } from 'react';
import { EventsOn } from '../wailsjs/runtime/runtime';

export type Level = 'ERROR' | 'WARN' | 'INFO' | 'DEBUG';

/** One log record as the backend groups it: header, level line and stack trace together. */
export interface LogEntry {
    level: Level;
    text: string;
}

export interface Row extends LogEntry {
    id: number;
}

/** Enough history to cover a full startup without letting the DOM grow unbounded. */
const MAX_ENTRIES = 2000;
/** Records are collected and applied in batches: startup emits hundreds per second. */
const FLUSH_INTERVAL_MS = 120;

// The history lives here and not in the Dashboard on purpose: navigating away
// unmounts the console, and a component-local buffer would take the whole log
// with it. The backend only emits — it keeps no backlog — so an idle server
// would leave the console empty forever after a tab switch.
let rows: Row[] = [];
let pending: Row[] = [];
let nextId = 0;
const listeners = new Set<() => void>();

const notify = () => {
    for (const listener of listeners) listener();
};

EventsOn('tomee-log', (entry: LogEntry) => {
    pending.push({ ...entry, id: nextId++ });
});

setInterval(() => {
    if (pending.length === 0) return;
    rows = [...rows, ...pending].slice(-MAX_ENTRIES);
    pending = [];
    notify();
}, FLUSH_INTERVAL_MS);

const subscribe = (listener: () => void) => {
    listeners.add(listener);
    return () => {
        listeners.delete(listener);
    };
};

/** Subscribes to the shared log buffer. The snapshot is stable between flushes. */
export const useLogs = () => useSyncExternalStore(subscribe, () => rows);

export const clearLogs = () => {
    rows = [];
    pending = [];
    notify();
};
