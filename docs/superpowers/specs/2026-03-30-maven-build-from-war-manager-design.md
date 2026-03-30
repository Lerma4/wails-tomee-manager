# Maven Build from WAR Manager

## Overview

Add the ability to run `mvn clean install` directly from the WAR Manager list, with per-row loading state, build log modal, and visual WAR file existence check.

## Backend — `MavenService`

New file: `backend/service/maven.go`

### Struct

```go
type MavenService struct {
    storage *StorageService
    ctx     context.Context
    mu      sync.Mutex
    builds  map[int]*exec.Cmd // active builds keyed by WAR ID
}
```

### Methods

**`FindProjectRoot(sourcePath string) (string, error)`**
- Starts from the directory containing `sourcePath`
- Walks up the directory tree looking for `pom.xml`
- Returns the directory containing the first `pom.xml` found
- Error if: reaches filesystem root, or exceeds 10 levels of traversal

**`RunBuild(warID int) error`**
- Loads the WAR artifact from DB via `StorageService`
- Calls `FindProjectRoot` on its `sourcePath`
- Rejects if a build for this `warID` is already in progress
- Launches `mvn clean install` in the project root directory
- Streams stdout/stderr line-by-line via Wails event `maven-log-{warID}` (payload: string)
- On completion, emits `maven-done-{warID}` (payload: `{success: bool, error: string}`)
- Removes entry from `builds` map on completion
- Uses mutex to protect `builds` map

**`CheckWarExists(sourcePath string) bool`**
- Runs `os.Stat(sourcePath)`
- Returns `true` if file exists and is a regular file, `false` otherwise

### Registration

- Created in `main.go` via `NewMavenService(storageService)`
- `SetContext(ctx)` called in `OnStartup`, same pattern as `TomEEService`
- Bound to frontend in `Bind` slice

## Frontend — `WarManager.tsx` Changes

### New per-row state

Each WAR row tracks independently:
- `buildState`: `"idle"` | `"building"` | `"success"` | `"error"`
- `logBuffer`: `string[]` — accumulated build output lines
- `warExists`: `boolean | null` — WAR file existence (`null` = not yet checked)

### Table columns (updated)

| Status | Source Path | WAR File | Destination | Build | Actions |
|--------|-------------|----------|-------------|-------|---------|

**WAR File column:**
- Green check icon if `warExists === true`
- Red X icon if `warExists === false`
- Spinner if `warExists === null` (checking)

**Build column:**
- Hammer/wrench icon button when `buildState === "idle"`
- Spinner when `buildState === "building"` (clickable to open log modal)
- Green check briefly on `"success"`, red X briefly on `"error"`, then back to idle

### Header buttons (updated)

Existing: "Add WAR", "Deploy All"
New: "Refresh" button — calls `CheckWarExists` for all WARs and updates `warExists` state

### Build log modal

- Triggered by clicking the spinner/build area of a row during or after build
- Header: WAR `destName` + status badge (Building.../Success/Error)
- Body: monospaced, scrollable `<pre>` area with build output
- Auto-scrolls to bottom as new lines arrive
- Listens to Wails event `maven-log-{warID}` for real-time updates
- "Close" button in footer

### Event flow

1. User clicks Build on row → calls `MavenService.RunBuild(warID)`
2. Row transitions to `buildState: "building"`, starts collecting `maven-log-{warID}` events
3. On `maven-done-{warID}`: row transitions to `"success"` or `"error"`, re-checks `CheckWarExists`
4. After brief delay (2s), row returns to `"idle"`

### WAR existence check triggers

1. On component mount (initial `fetchWars`)
2. After each `maven-done-{warID}` event (for that specific WAR)
3. On manual "Refresh" button click (all WARs)

## Concurrency

- **Go side:** `MavenService.builds` map protected by mutex. One build per WAR ID at a time. Different WARs can build concurrently.
- **Frontend side:** Each row manages its own state independently. Multiple rows can show building state simultaneously.

## Error handling

- `FindProjectRoot`: returns error if no `pom.xml` found within 10 levels or at filesystem root
- `RunBuild`: returns error immediately if WAR not found in DB, project root not found, or build already in progress for that WAR
- Build process failure: captured via exit code, reported through `maven-done-{warID}` event with `success: false` and error message
- Frontend: shows error state on row, full error visible in log modal
