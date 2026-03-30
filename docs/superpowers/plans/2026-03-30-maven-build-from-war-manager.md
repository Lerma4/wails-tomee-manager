# Maven Build from WAR Manager — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `mvn clean install` build capability per-WAR in the WAR Manager, with per-row loading, build log modal, and WAR file existence check.

**Architecture:** New `MavenService` Go backend service handles build execution and WAR existence checks. It streams output via Wails events (`maven-log-{warID}`, `maven-done-{warID}`). Frontend `WarManager.tsx` gains per-row build state, a WAR existence indicator column, a build log modal, and a refresh button.

**Tech Stack:** Go (backend service), Wails v2 events, React 19, TypeScript, DaisyUI v5, react-icons

---

### Task 1: Add `GetWar` method to `StorageService`

**Files:**
- Modify: `backend/service/storage.go`

`MavenService.RunBuild` needs to load a single WAR by ID. `StorageService` currently only has `ListWars`. Add a `GetWar` method.

- [ ] **Step 1: Add `GetWar` method to `StorageService`**

In `backend/service/storage.go`, add after the `DeleteWar` method (line 101):

```go
func (s *StorageService) GetWar(id int) (model.WarArtifact, error) {
	var war model.WarArtifact
	row := s.db.QueryRow(`SELECT id, source_path, dest_name, enabled FROM wars WHERE id=?`, id)
	err := row.Scan(&war.ID, &war.SourcePath, &war.DestName, &war.Enabled)
	return war, err
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./...`
Expected: compiles successfully with no errors.

- [ ] **Step 3: Commit**

```bash
git add backend/service/storage.go
git commit -m "feat: add GetWar method to StorageService"
```

---

### Task 2: Create `MavenService` backend

**Files:**
- Create: `backend/service/maven.go`

This is the core backend service. It provides `FindProjectRoot`, `RunBuild`, and `CheckWarExists`.

- [ ] **Step 1: Create `backend/service/maven.go`**

```go
package service

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type MavenService struct {
	storage *StorageService
	ctx     context.Context
	mu      sync.Mutex
	builds  map[int]*exec.Cmd
}

func NewMavenService(storage *StorageService) *MavenService {
	return &MavenService{
		storage: storage,
		builds:  make(map[int]*exec.Cmd),
	}
}

func (s *MavenService) SetContext(ctx context.Context) {
	s.ctx = ctx
}

// FindProjectRoot walks up from the directory containing sourcePath
// looking for a pom.xml. Returns error after 10 levels or at filesystem root.
func (s *MavenService) FindProjectRoot(sourcePath string) (string, error) {
	dir := filepath.Dir(sourcePath)
	const maxLevels = 10

	for i := 0; i < maxLevels; i++ {
		pomPath := filepath.Join(dir, "pom.xml")
		if _, err := os.Stat(pomPath); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			return "", fmt.Errorf("no pom.xml found: reached filesystem root from %s", sourcePath)
		}
		dir = parent
	}

	return "", fmt.Errorf("no pom.xml found within %d levels above %s", maxLevels, sourcePath)
}

// CheckWarExists returns true if the file at sourcePath exists and is a regular file.
func (s *MavenService) CheckWarExists(sourcePath string) bool {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// RunBuild executes `mvn clean install` in the project root derived from the WAR's sourcePath.
// Streams output via Wails events. Only one build per WAR ID at a time.
func (s *MavenService) RunBuild(warID int) error {
	war, err := s.storage.GetWar(warID)
	if err != nil {
		return fmt.Errorf("WAR artifact not found: %w", err)
	}

	projectRoot, err := s.FindProjectRoot(war.SourcePath)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if _, exists := s.builds[warID]; exists {
		s.mu.Unlock()
		return fmt.Errorf("build already in progress for WAR %d", warID)
	}

	var mvnCmd string
	if runtime.GOOS == "windows" {
		mvnCmd = "mvn.cmd"
	} else {
		mvnCmd = "mvn"
	}

	cmd := exec.Command(mvnCmd, "clean", "install")
	cmd.Dir = projectRoot
	cmd.Env = os.Environ()

	s.builds[warID] = cmd
	s.mu.Unlock()

	logEvent := fmt.Sprintf("maven-log-%d", warID)
	doneEvent := fmt.Sprintf("maven-done-%d", warID)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.removeBuild(warID)
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		s.removeBuild(warID)
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		s.removeBuild(warID)
		return fmt.Errorf("failed to start mvn: %w", err)
	}

	// Stream stdout and stderr in background goroutines
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if s.ctx != nil {
				wailsRuntime.EventsEmit(s.ctx, logEvent, scanner.Text())
			}
		}
	}()
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			if s.ctx != nil {
				wailsRuntime.EventsEmit(s.ctx, logEvent, scanner.Text())
			}
		}
	}()

	// Wait for process completion in background, then emit done event
	go func() {
		wg.Wait()
		waitErr := cmd.Wait()
		s.removeBuild(warID)

		result := map[string]interface{}{
			"success": waitErr == nil,
			"error":   "",
		}
		if waitErr != nil {
			result["error"] = waitErr.Error()
		}
		if s.ctx != nil {
			wailsRuntime.EventsEmit(s.ctx, doneEvent, result)
		}
	}()

	return nil
}

func (s *MavenService) removeBuild(warID int) {
	s.mu.Lock()
	delete(s.builds, warID)
	s.mu.Unlock()
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./...`
Expected: compiles successfully.

- [ ] **Step 3: Commit**

```bash
git add backend/service/maven.go
git commit -m "feat: add MavenService with build and WAR existence check"
```

---

### Task 3: Register `MavenService` in app startup

**Files:**
- Modify: `main.go`
- Modify: `app.go`

Wire `MavenService` into the Wails app, same pattern as `TomEEService`.

- [ ] **Step 1: Update `App` struct and `NewApp` to accept `MavenService`**

In `app.go`, update the struct and constructor:

```go
type App struct {
	ctx          context.Context
	tomeeService *service.TomEEService
	mavenService *service.MavenService
}

func NewApp(tomeeService *service.TomEEService, mavenService *service.MavenService) *App {
	return &App{
		tomeeService: tomeeService,
		mavenService: mavenService,
	}
}
```

In `app.go`, update the `startup` method to also set context on `MavenService`:

```go
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.tomeeService.SetContext(ctx)
	a.mavenService.SetContext(ctx)
}
```

- [ ] **Step 2: Update `main.go` to create and bind `MavenService`**

In `main.go`, after `warService` creation (line 18), add:

```go
mavenService := service.NewMavenService(storageService)
```

Update `NewApp` call:

```go
app := NewApp(tomeeService, mavenService)
```

Add `mavenService` to the `Bind` slice:

```go
Bind: []interface{}{
    app,
    storageService,
    tomeeService,
    warService,
    mavenService,
},
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./...`
Expected: compiles successfully.

- [ ] **Step 4: Commit**

```bash
git add app.go main.go
git commit -m "feat: register MavenService in Wails app"
```

---

### Task 4: Regenerate Wails bindings

**Files:**
- Auto-generated: `frontend/wailsjs/go/service/MavenService.js` and `.d.ts`

Wails needs to regenerate TypeScript bindings so the frontend can call `MavenService` methods.

- [ ] **Step 1: Generate bindings**

Run: `wails generate module`

If that doesn't work, run: `wails dev` briefly and stop it (Ctrl+C) — this regenerates bindings on startup.

- [ ] **Step 2: Verify bindings exist**

Check that `frontend/wailsjs/go/service/MavenService.js` and `MavenService.d.ts` now exist and contain:

```typescript
export function CheckWarExists(arg1: string): Promise<boolean>;
export function RunBuild(arg1: number): Promise<void>;
```

Note: `FindProjectRoot` and `SetContext` will also be generated but we won't use them from the frontend.

- [ ] **Step 3: Commit**

```bash
git add frontend/wailsjs/
git commit -m "chore: regenerate Wails bindings for MavenService"
```

---

### Task 5: Update `WarManager.tsx` — WAR existence check + refresh button

**Files:**
- Modify: `frontend/src/pages/WarManager.tsx`

Add the WAR existence column and the refresh button in the header. This task does NOT add the build functionality yet — that's Task 6.

- [ ] **Step 1: Add imports**

At the top of `WarManager.tsx`, add:

```typescript
import { CheckWarExists } from '../../wailsjs/go/service/MavenService';
import { FaPlus, FaTrash, FaEdit, FaRocket, FaFolder, FaBoxOpen, FaSync, FaCheckCircle, FaTimesCircle } from 'react-icons/fa';
```

Remove the old `react-icons/fa` import that doesn't include the new icons.

- [ ] **Step 2: Add `warExistsMap` state and check function**

After the existing state declarations (line 12), add:

```typescript
const [warExistsMap, setWarExistsMap] = useState<Record<number, boolean | null>>({});
const [refreshing, setRefreshing] = useState(false);

const checkAllWarExists = async (warList: model.WarArtifact[]) => {
    const results: Record<number, boolean | null> = {};
    for (const w of warList) {
        results[w.id] = null; // loading
    }
    setWarExistsMap(results);

    for (const w of warList) {
        try {
            const exists = await CheckWarExists(w.sourcePath);
            setWarExistsMap(prev => ({ ...prev, [w.id]: exists }));
        } catch {
            setWarExistsMap(prev => ({ ...prev, [w.id]: false }));
        }
    }
};
```

- [ ] **Step 3: Update `fetchWars` to also check existence**

Replace the existing `fetchWars`:

```typescript
const fetchWars = () => {
    ListWars().then((data) => {
        const list = data || [];
        setWars(list);
        checkAllWarExists(list);
    }).catch(console.error);
};
```

- [ ] **Step 4: Add refresh button handler**

```typescript
const handleRefresh = async () => {
    setRefreshing(true);
    await checkAllWarExists(wars);
    setRefreshing(false);
};
```

- [ ] **Step 5: Add Refresh button in the header**

In the header `<div className="flex gap-2">`, add before the "Add WAR" button:

```tsx
<button
    className="btn btn-ghost btn-sm gap-2"
    onClick={handleRefresh}
    disabled={refreshing}
    title="Refresh WAR status"
>
    {refreshing
        ? <span className="loading loading-spinner loading-xs" />
        : <FaSync className="text-xs" />}
    Refresh
</button>
```

- [ ] **Step 6: Add "WAR File" column to the table**

Update the `<thead>`:

```tsx
<thead>
    <tr>
        <th className="w-20">Status</th>
        <th>Source Path</th>
        <th className="w-20 text-center">WAR File</th>
        <th>Destination</th>
        <th className="w-24 text-right">Actions</th>
    </tr>
</thead>
```

In each `<tr>` row, after the Source Path `<td>`, add:

```tsx
<td className="text-center">
    {warExistsMap[war.id] === null
        ? <span className="loading loading-spinner loading-xs" />
        : warExistsMap[war.id]
            ? <FaCheckCircle className="text-success inline" />
            : <FaTimesCircle className="text-error inline" />}
</td>
```

- [ ] **Step 7: Verify compilation**

Run (from `frontend/`): `npm run build`
Expected: compiles successfully.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/pages/WarManager.tsx
git commit -m "feat: add WAR file existence check and refresh button"
```

---

### Task 6: Update `WarManager.tsx` — Maven build per-row with log modal

**Files:**
- Modify: `frontend/src/pages/WarManager.tsx`

Add the build button per row, per-row build state, and the build log modal.

- [ ] **Step 1: Add build-related imports**

Add to the imports:

```typescript
import { CheckWarExists, RunBuild } from '../../wailsjs/go/service/MavenService';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { FaPlus, FaTrash, FaEdit, FaRocket, FaFolder, FaBoxOpen, FaSync, FaCheckCircle, FaTimesCircle, FaHammer } from 'react-icons/fa';
```

(This replaces the imports from Task 5 — consolidate all icons in one import.)

- [ ] **Step 2: Add per-row build state**

After the existing state declarations, add:

```typescript
type BuildState = 'idle' | 'building' | 'success' | 'error';

const [buildStates, setBuildStates] = useState<Record<number, BuildState>>({});
const [buildLogs, setBuildLogs] = useState<Record<number, string[]>>({});
const [logModalWarId, setLogModalWarId] = useState<number | null>(null);
```

- [ ] **Step 3: Add build handler**

```typescript
const handleBuild = async (warId: number) => {
    setBuildStates(prev => ({ ...prev, [warId]: 'building' }));
    setBuildLogs(prev => ({ ...prev, [warId]: [] }));

    try {
        await RunBuild(warId);
    } catch (err) {
        setBuildStates(prev => ({ ...prev, [warId]: 'error' }));
        setBuildLogs(prev => ({
            ...prev,
            [warId]: [...(prev[warId] || []), `Error: ${err}`],
        }));
    }
};
```

- [ ] **Step 4: Add event listeners for build log and done**

Inside the component, add a `useEffect` that sets up listeners for all active builds:

```typescript
useEffect(() => {
    const cleanups: (() => void)[] = [];

    wars.forEach((war) => {
        const logEvent = `maven-log-${war.id}`;
        const doneEvent = `maven-done-${war.id}`;

        const cancelLog = EventsOn(logEvent, (line: string) => {
            setBuildLogs(prev => ({
                ...prev,
                [war.id]: [...(prev[war.id] || []), line],
            }));
        });

        const cancelDone = EventsOn(doneEvent, (result: { success: boolean; error: string }) => {
            const newState: BuildState = result.success ? 'success' : 'error';
            setBuildStates(prev => ({ ...prev, [war.id]: newState }));

            if (!result.success && result.error) {
                setBuildLogs(prev => ({
                    ...prev,
                    [war.id]: [...(prev[war.id] || []), `BUILD FAILED: ${result.error}`],
                }));
            }

            // Re-check WAR existence after build completes
            CheckWarExists(war.sourcePath).then(exists => {
                setWarExistsMap(prev => ({ ...prev, [war.id]: exists }));
            });

            // Reset to idle after 3 seconds
            setTimeout(() => {
                setBuildStates(prev => ({ ...prev, [war.id]: 'idle' }));
            }, 3000);
        });

        cleanups.push(cancelLog, cancelDone);
    });

    return () => { cleanups.forEach(fn => fn()); };
}, [wars]);
```

- [ ] **Step 5: Add "Build" column to the table**

Update `<thead>` to include Build column between Destination and Actions:

```tsx
<thead>
    <tr>
        <th className="w-20">Status</th>
        <th>Source Path</th>
        <th className="w-20 text-center">WAR File</th>
        <th>Destination</th>
        <th className="w-20 text-center">Build</th>
        <th className="w-24 text-right">Actions</th>
    </tr>
</thead>
```

In each row, after the Destination `<td>`, add:

```tsx
<td className="text-center">
    {buildStates[war.id] === 'building' ? (
        <button
            className="btn btn-ghost btn-xs"
            onClick={() => setLogModalWarId(war.id)}
            title="View build log"
        >
            <span className="loading loading-spinner loading-xs" />
        </button>
    ) : buildStates[war.id] === 'success' ? (
        <button
            className="btn btn-ghost btn-xs text-success"
            onClick={() => setLogModalWarId(war.id)}
            title="Build succeeded — view log"
        >
            <FaCheckCircle />
        </button>
    ) : buildStates[war.id] === 'error' ? (
        <button
            className="btn btn-ghost btn-xs text-error"
            onClick={() => setLogModalWarId(war.id)}
            title="Build failed — view log"
        >
            <FaTimesCircle />
        </button>
    ) : (
        <button
            className="btn btn-ghost btn-xs"
            onClick={() => handleBuild(war.id)}
            title="Run mvn clean install"
        >
            <FaHammer />
        </button>
    )}
</td>
```

- [ ] **Step 6: Add the build log modal**

After the existing edit/add modal (`{modalOpen && (...)}`), add:

```tsx
{logModalWarId !== null && (
    <BuildLogModal
        warId={logModalWarId}
        wars={wars}
        buildStates={buildStates}
        buildLogs={buildLogs}
        onClose={() => setLogModalWarId(null)}
    />
)}
```

Define `BuildLogModal` as a component inside the same file, before the `WarManager` component:

```tsx
const BuildLogModal = ({
    warId,
    wars,
    buildStates,
    buildLogs,
    onClose,
}: {
    warId: number;
    wars: model.WarArtifact[];
    buildStates: Record<number, BuildState>;
    buildLogs: Record<number, string[]>;
    onClose: () => void;
}) => {
    const logsEndRef = useRef<HTMLDivElement>(null);
    const war = wars.find(w => w.id === warId);
    const state = buildStates[warId] || 'idle';
    const logs = buildLogs[warId] || [];

    useEffect(() => {
        logsEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    }, [logs]);

    const statusBadge = state === 'building'
        ? <span className="badge badge-info badge-sm gap-1"><span className="loading loading-spinner loading-xs" />Building...</span>
        : state === 'success'
            ? <span className="badge badge-success badge-sm">Success</span>
            : state === 'error'
                ? <span className="badge badge-error badge-sm">Error</span>
                : <span className="badge badge-ghost badge-sm">Idle</span>;

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center modal-backdrop-blur">
            <div className="panel p-0 w-full max-w-3xl mx-4 flex flex-col" style={{ maxHeight: '80vh' }}>
                <div className="flex items-center justify-between p-4 border-b border-base-content/5">
                    <div className="flex items-center gap-3">
                        <h3 className="text-lg font-bold tracking-tight">
                            Build Log — {war?.destName || `WAR #${warId}`}
                        </h3>
                        {statusBadge}
                    </div>
                    <button className="btn btn-ghost btn-sm" onClick={onClose}>Close</button>
                </div>
                <div className="terminal-body flex-1 overflow-y-auto p-4 font-mono text-xs" style={{ minHeight: '300px' }}>
                    {logs.length === 0 && (
                        <div className="log-placeholder">Waiting for build output...</div>
                    )}
                    {logs.map((line, i) => (
                        <div key={i} className="log-line">{line}</div>
                    ))}
                    <div ref={logsEndRef} />
                </div>
            </div>
        </div>
    );
};
```

Note: add `useRef` to the React imports at the top if not already there:

```typescript
import { useEffect, useState, useRef } from 'react';
```

- [ ] **Step 7: Verify compilation**

Run (from `frontend/`): `npm run build`
Expected: compiles successfully.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/pages/WarManager.tsx
git commit -m "feat: add per-row Maven build with log modal in WAR Manager"
```

---

### Task 7: Manual smoke test

**Files:** None (verification only)

- [ ] **Step 1: Start the app**

Run: `wails dev`

- [ ] **Step 2: Verify WAR existence check**

Navigate to WAR Manager. Each WAR row should show a green check or red X in the "WAR File" column. The "Refresh" button should re-check all.

- [ ] **Step 3: Verify Maven build**

Click the hammer icon on a WAR whose project has a `pom.xml`. The row should show a spinner. Click the spinner to open the log modal — build output should stream in real time. On completion, the status should show success or error, and the WAR existence icon should update.

- [ ] **Step 4: Verify concurrent builds**

Start builds on two different WARs simultaneously. Both should show independent spinners and have separate log modals.

- [ ] **Step 5: Verify error handling**

Test with a WAR whose `sourcePath` has no `pom.xml` within 10 levels. The build should fail immediately with a clear error.

- [ ] **Step 6: Final commit (if any adjustments needed)**

```bash
git add -A
git commit -m "fix: adjustments from smoke testing Maven build feature"
```
