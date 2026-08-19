package service

// End-to-end runs against a real TomEE installation. They skip unless
// LIVE_TOMEE points at one:
//
//	LIVE_TOMEE=/path/to/apache-tomee LIVE_JAVA_HOME=/path/to/jdk go test ./backend/service/ -run TestLive -v
//
// Everything happens in an isolated CATALINA_BASE under a temporary directory
// and on ports 18080/18005/18000, so the installation and any server already
// running on the usual ports are left alone.

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"tomee-manager/backend/model"

	"github.com/beevik/etree"
)

// Live end-to-end run against a real TomEE installation. Opt in with
// LIVE_TOMEE=<path to installation>.
func TestLiveStartAndStop(t *testing.T) {
	install := os.Getenv("LIVE_TOMEE")
	if install == "" {
		t.Skip("set LIVE_TOMEE to run against a real installation")
	}

	home := t.TempDir()
	t.Setenv("APPDATA", home)
	t.Setenv("XDG_CONFIG_HOME", home)

	storage := NewStorageService()
	if err := storage.open(filepath.Join(t.TempDir(), "live.db")); err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer func() { _ = storage.db.Close() }()

	config := model.Config{
		TomEEPath:    install,
		JavaHome:     os.Getenv("LIVE_JAVA_HOME"),
		HTTPPort:     18080,
		ShutdownPort: 18005,
		DebugPort:    18000,
		VMOptions:    "-Xmx512m -Dtomee.manager.livetest=yes",
		IsolatedBase: true,
	}
	if err := storage.SaveConfig(config); err != nil {
		t.Fatalf("save config: %v", err)
	}

	svc := NewTomEEService(storage)
	if err := svc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if svc.IsRunning() {
			_ = svc.Stop()
		}
	})

	base, err := instanceDir(config)
	if err != nil {
		t.Fatalf("instanceDir: %v", err)
	}
	t.Logf("CATALINA_BASE = %s", base)
	if base == install {
		t.Fatal("live run used the installation as CATALINA_BASE")
	}

	// The rewritten ports must have landed in the isolated copy, not the install.
	doc := etree.NewDocument()
	if err := doc.ReadFromFile(filepath.Join(base, "conf", "server.xml")); err != nil {
		t.Fatalf("read isolated server.xml: %v", err)
	}
	if got := doc.FindElement("//Server").SelectAttrValue("port", ""); got != "18005" {
		t.Errorf("isolated shutdown port = %q, want 18005", got)
	}
	var httpPort string
	for _, c := range doc.FindElements("//Connector") {
		if isPlainHTTPConnector(c) {
			httpPort = c.SelectAttrValue("port", "")
			break
		}
	}
	if httpPort != "18080" {
		t.Errorf("isolated http connector port = %q, want 18080", httpPort)
	}

	installDoc := etree.NewDocument()
	if err := installDoc.ReadFromFile(filepath.Join(install, "conf", "server.xml")); err != nil {
		t.Fatalf("read installation server.xml: %v", err)
	}
	if got := installDoc.FindElement("//Server").SelectAttrValue("port", ""); got == "18005" {
		t.Error("the live run modified the installation server.xml")
	}

	// IsRunning is true as soon as the process exists; wait for the server to
	// actually answer before asking it to stop.
	deadline := time.Now().Add(120 * time.Second)
	for !httpAlive(18080) {
		if time.Now().After(deadline) {
			t.Fatal("TomEE never came up on port 18080")
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Log("server answered on 18080")

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := waitForPortFree(18080, 60*time.Second); err != nil {
		t.Errorf("port not released: %v", err)
	}

	// Parse the log this very run produced and report what the console would show.
	logs, err := filepath.Glob(filepath.Join(base, "logs", "catalina.*.log"))
	if err != nil || len(logs) == 0 {
		t.Fatalf("no catalina log written to the isolated base (err=%v)", err)
	}
	raw, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	multiline := 0
	var startupSeen bool
	streamEntries(strings.NewReader(string(raw)), func(e LogEntry) {
		counts[e.Level]++
		if strings.Contains(e.Text, "\n") {
			multiline++
		}
		if strings.Contains(e.Text, startupMarker) {
			startupSeen = true
			t.Logf("startup record (level %s): %s", e.Level, e.Text)
		}
	})
	t.Logf("live log: %v, multiline records: %d", counts, multiline)
	if !startupSeen {
		t.Error("the startup marker was never parsed out of the live log")
	}
}

// Deploys a webapp with a context descriptor pointing outside webapps/ and
// checks the real server serves it. Opt in with LIVE_TOMEE.
func TestLiveExplodedDeployIsServed(t *testing.T) {
	install := os.Getenv("LIVE_TOMEE")
	if install == "" {
		t.Skip("set LIVE_TOMEE to run against a real installation")
	}

	// A webapp needs nothing but a WEB-INF directory to be deployable under
	// Servlet 3.0, so this stays independent of any real project.
	project := t.TempDir()
	exploded := filepath.Join(project, "target", "App")
	if err := os.MkdirAll(filepath.Join(exploded, "WEB-INF"), 0o755); err != nil {
		t.Fatal(err)
	}
	const marker = "served-from-target-without-copying"
	if err := os.WriteFile(filepath.Join(exploded, "index.html"), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	t.Setenv("APPDATA", home)
	t.Setenv("XDG_CONFIG_HOME", home)

	storage := NewStorageService()
	if err := storage.open(filepath.Join(t.TempDir(), "deploy.db")); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = storage.db.Close() }()

	config := model.Config{
		TomEEPath: install, JavaHome: os.Getenv("LIVE_JAVA_HOME"),
		HTTPPort: 18080, ShutdownPort: 18005, DebugPort: 18000,
		IsolatedBase: true,
	}
	if err := storage.SaveConfig(config); err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveWar(model.WarArtifact{
		SourcePath: project, DestName: "app.war", Enabled: true, DeployMode: model.DeployExploded,
	}); err != nil {
		t.Fatal(err)
	}

	wars, err := storage.ListWars()
	if err != nil || len(wars) != 1 {
		t.Fatalf("ListWars = %+v, %v", wars, err)
	}
	warID := wars[0].ID

	warSvc := NewWarService(storage)
	if err := warSvc.DeployAll(); err != nil {
		t.Fatalf("DeployAll: %v", err)
	}

	deployed, err := warSvc.IsDeployed(warID)
	if err != nil || !deployed {
		t.Fatalf("IsDeployed = %v, %v; want true", deployed, err)
	}

	base, err := instanceDir(config)
	if err != nil {
		t.Fatal(err)
	}
	// Nothing may have been copied into webapps/: that is the whole point.
	if _, err := os.Stat(filepath.Join(webappsDir(base), "app.war")); err == nil {
		t.Error("a .war was copied into webapps/ in exploded mode")
	}
	descriptor := filepath.Join(contextDir(base), "app.xml")
	raw, err := os.ReadFile(descriptor)
	if err != nil {
		t.Fatalf("read descriptor: %v", err)
	}
	t.Logf("descriptor: %s", strings.TrimSpace(string(raw)))

	svc := NewTomEEService(storage)
	if err := svc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if svc.IsRunning() {
			_ = svc.Stop()
		}
		_ = waitForPortFree(18080, 60*time.Second)
	})

	deadline := time.Now().Add(120 * time.Second)
	for !httpAlive(18080) {
		if time.Now().After(deadline) {
			t.Fatal("TomEE never came up")
		}
		time.Sleep(500 * time.Millisecond)
	}

	// The context deploys a moment after the connector answers.
	var body string
	var status int
	for time.Now().Before(deadline) {
		status, body = get(t, "http://127.0.0.1:18080/app/index.html")
		if status == http.StatusOK {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if status != http.StatusOK {
		t.Fatalf("GET /app/index.html = %d, want 200 (body %q)", status, body)
	}
	if !strings.Contains(body, marker) {
		t.Errorf("body = %q, want it to contain %q", body, marker)
	}
	t.Log("the exploded build output was served straight from target/, nothing copied")

	// Editing the build output must show up without redeploying: that is what
	// exploded mode buys.
	const edited = "edited-in-place-no-redeploy"
	if err := os.WriteFile(filepath.Join(exploded, "index.html"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline) {
		_, body = get(t, "http://127.0.0.1:18080/app/index.html")
		if strings.Contains(body, edited) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !strings.Contains(body, edited) {
		t.Errorf("edited file not picked up; body = %q", body)
	}

	// Re-deploying while the server runs must not fail, even though Tomcat holds
	// the descriptor open on Windows: an unchanged descriptor is left alone.
	if err := warSvc.DeployAll(); err != nil {
		t.Errorf("DeployAll while running: %v", err)
	}

	// Undeploying while the server runs cannot work on Windows, and the error has
	// to say so rather than surfacing a bare sharing violation.
	err = warSvc.Undeploy(warID)
	if err == nil {
		t.Log("undeploy succeeded while running (no file lock on this platform)")
	} else if !strings.Contains(err.Error(), "stop TomEE first") {
		t.Errorf("unhelpful undeploy error while running: %v", err)
	}

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := waitForPortFree(18080, 60*time.Second); err != nil {
		t.Fatalf("port not released: %v", err)
	}

	// With the server down, undeploy removes the descriptor and leaves the build
	// output alone.
	if err := warSvc.Undeploy(warID); err != nil {
		t.Fatalf("Undeploy after stop: %v", err)
	}
	if _, err := os.Stat(descriptor); err == nil {
		t.Error("the context descriptor survived Undeploy")
	}
	if _, err := os.Stat(filepath.Join(exploded, "index.html")); err != nil {
		t.Errorf("Undeploy deleted the build output: %v", err)
	}
	deployed, err = warSvc.IsDeployed(warID)
	if err != nil || deployed {
		t.Errorf("IsDeployed after Undeploy = %v, %v; want false", deployed, err)
	}
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return 0, fmt.Sprintf("request error: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// Restart has to wait for every port the old JVM held, not just HTTP: Tomcat
// closes its connectors first and releases the shutdown port last.
func TestLiveRestart(t *testing.T) {
	install := os.Getenv("LIVE_TOMEE")
	if install == "" {
		t.Skip("set LIVE_TOMEE to run against a real installation")
	}

	home := t.TempDir()
	t.Setenv("APPDATA", home)
	t.Setenv("XDG_CONFIG_HOME", home)

	storage := NewStorageService()
	if err := storage.open(filepath.Join(t.TempDir(), "restart.db")); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = storage.db.Close() }()

	config := model.Config{
		TomEEPath: install, JavaHome: os.Getenv("LIVE_JAVA_HOME"),
		HTTPPort: 18080, ShutdownPort: 18005, DebugPort: 18000,
		IsolatedBase: true,
	}
	if err := storage.SaveConfig(config); err != nil {
		t.Fatal(err)
	}

	svc := NewTomEEService(storage)
	if err := svc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if svc.IsRunning() {
			_ = svc.Stop()
		}
		_ = waitForPortFree(18080, 60*time.Second)
	})
	waitUntilServing(t, 18080)

	// Repeat it: a restart that races the old JVM tends to fail intermittently,
	// so once proves little.
	for i := 1; i <= 2; i++ {
		start := time.Now()
		if err := svc.Restart(); err != nil {
			t.Fatalf("Restart %d: %v", i, err)
		}
		waitUntilServing(t, 18080)
		t.Logf("restart %d came back up in %v", i, time.Since(start).Round(time.Millisecond))
	}

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := waitForPortsFree(config, 60*time.Second); err != nil {
		t.Errorf("ports not released: %v", err)
	}
}

// waitUntilServing blocks until the server answers, failing the test on timeout.
func waitUntilServing(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for !httpAlive(port) {
		if time.Now().After(deadline) {
			t.Fatalf("TomEE never answered on port %d", port)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
