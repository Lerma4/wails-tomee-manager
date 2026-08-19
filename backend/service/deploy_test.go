package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"tomee-manager/backend/model"
)

// fakeProject builds a Maven-style output tree: target/App.war next to the
// exploded target/App/WEB-INF.
func fakeProject(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(filepath.Join(target, name, "WEB-INF"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, name+".war"), []byte("PK-war"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// fakeBase builds the CATALINA_BASE layout deploys write into.
func fakeBase(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	for _, d := range []string{webappsDir(base), contextDir(base)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return base
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestContextName(t *testing.T) {
	cases := map[string]string{
		"logistico.war":   "logistico",
		"CommercialePlus": "CommercialePlus",
		"App.WAR":         "App",
		"ROOT.war":        "ROOT",
		"root":            "ROOT",
		"":                "ROOT",
		"  spaced.war  ":  "spaced",
		"manager#foo.war": "manager#foo",
		// Context paths as a person would actually write them.
		"/commerciale":     "commerciale",
		"/commerciale.war": "commerciale",
		"/":                "ROOT",
		"api/v1":           "api#v1",
		"/api/v1/":         "api#v1",
		`\commerciale`:     "commerciale",
	}
	for in, want := range cases {
		if got := contextName(in); got != want {
			t.Errorf("contextName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateContextPath(t *testing.T) {
	for _, bad := range []string{"", "   ", "../evil.war", `..\evil.war`, "a/../b", "bad:name", "q?x", `quo"te`} {
		if err := validateContextPath(bad); err == nil {
			t.Errorf("validateContextPath(%q) = nil; want error", bad)
		}
	}
	// Slashes are how a context path is normally written, so they must pass.
	for _, ok := range []string{"app.war", "ROOT.war", "manager#foo.war", "NoExtension", "/commerciale", "api/v1", "/"} {
		if err := validateContextPath(ok); err != nil {
			t.Errorf("validateContextPath(%q) = %v; want nil", ok, err)
		}
	}
}

// The whole point of a context path: the artifact keeps the name Maven gave it
// while the app is served somewhere else.
func TestContextPathIsIndependentOfTheArtifactName(t *testing.T) {
	base, project := fakeBase(t), fakeProject(t, "CommercialePlus")
	war := model.WarArtifact{SourcePath: project, DestName: "/commerciale", DeployMode: model.DeployWar}

	if err := deployWar(base, war); err != nil {
		t.Fatalf("deployWar: %v", err)
	}
	descriptor := filepath.Join(contextDir(base), "commerciale.xml")
	raw, err := os.ReadFile(descriptor)
	if err != nil {
		t.Fatalf("expected a descriptor at %s: %v", descriptor, err)
	}
	wantDoc := filepath.Join(project, "target", "CommercialePlus.war")
	if !strings.Contains(string(raw), escapeForXMLAttr(wantDoc)) {
		t.Errorf("descriptor should point at the untouched artifact %s:\n%s", wantDoc, raw)
	}
}

// Changing the context path has to take the old one down, or the app answers on
// both URLs and nobody can tell which one is live.
func TestChangingTheContextPathRemovesTheOldOne(t *testing.T) {
	storage := openTempStorage(t)

	install := t.TempDir()
	if err := os.MkdirAll(filepath.Join(install, "webapps"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveConfig(model.Config{TomEEPath: install, HTTPPort: 8080}); err != nil {
		t.Fatal(err)
	}

	project := fakeProject(t, "CommercialePlus")
	if err := storage.SaveWar(model.WarArtifact{
		SourcePath: project, DestName: "CommercialePlus.war", Enabled: true, DeployMode: model.DeployCopy,
	}); err != nil {
		t.Fatal(err)
	}

	warSvc := NewWarService(storage)
	if err := warSvc.DeployAll(); err != nil {
		t.Fatalf("first DeployAll: %v", err)
	}
	old := filepath.Join(install, "webapps", "CommercialePlus.war")
	if !exists(old) {
		t.Fatalf("%s was not deployed", old)
	}

	wars, err := storage.ListWars()
	if err != nil || len(wars) != 1 {
		t.Fatalf("ListWars = %+v, %v", wars, err)
	}
	if wars[0].DeployedAs != "CommercialePlus" {
		t.Errorf("DeployedAs = %q, want CommercialePlus", wars[0].DeployedAs)
	}

	renamed := wars[0]
	renamed.DestName = "/commerciale"
	if err := storage.SaveWar(renamed); err != nil {
		t.Fatal(err)
	}
	if err := warSvc.DeployAll(); err != nil {
		t.Fatalf("second DeployAll: %v", err)
	}

	if exists(old) {
		t.Error("the old context survived the rename: the app answers on both URLs")
	}
	if !exists(filepath.Join(install, "webapps", "commerciale.war")) {
		t.Error("the new context was not deployed")
	}
	wars, _ = storage.ListWars()
	if wars[0].DeployedAs != "commerciale" {
		t.Errorf("DeployedAs = %q, want commerciale", wars[0].DeployedAs)
	}

	// Undeploy clears the record too, so a later deploy does not try to remove
	// something that is already gone.
	if err := warSvc.Undeploy(wars[0].ID); err != nil {
		t.Fatalf("Undeploy: %v", err)
	}
	wars, _ = storage.ListWars()
	if wars[0].DeployedAs != "" {
		t.Errorf("DeployedAs = %q after Undeploy, want empty", wars[0].DeployedAs)
	}
	deployed, err := warSvc.IsDeployed(wars[0].ID)
	if err != nil || deployed {
		t.Errorf("IsDeployed = %v, %v; want false", deployed, err)
	}
}

func TestDeployCopyPlacesWarInWebapps(t *testing.T) {
	base, project := fakeBase(t), fakeProject(t, "App")
	war := model.WarArtifact{SourcePath: project, DestName: "app.war", DeployMode: model.DeployCopy}

	if err := deployWar(base, war); err != nil {
		t.Fatalf("deployWar: %v", err)
	}
	d := deploymentFor(base, war.DestName)
	if !exists(d.warFile) {
		t.Errorf("%s was not created", d.warFile)
	}
	if exists(d.descriptor) {
		t.Errorf("%s should not exist in copy mode", d.descriptor)
	}
}

func TestDeployWarModeWritesDescriptorPointingAtTheBuiltWar(t *testing.T) {
	base, project := fakeBase(t), fakeProject(t, "App")
	war := model.WarArtifact{SourcePath: project, DestName: "app.war", DeployMode: model.DeployWar}

	if err := deployWar(base, war); err != nil {
		t.Fatalf("deployWar: %v", err)
	}
	d := deploymentFor(base, war.DestName)
	if exists(d.warFile) {
		t.Errorf("%s must not be copied in descriptor mode", d.warFile)
	}
	raw, err := os.ReadFile(d.descriptor)
	if err != nil {
		t.Fatalf("read descriptor: %v", err)
	}
	wantDoc := filepath.Join(project, "target", "App.war")
	if !strings.Contains(string(raw), escapeForXMLAttr(wantDoc)) {
		t.Errorf("descriptor does not point at %s:\n%s", wantDoc, raw)
	}
}

func TestDeployExplodedPointsAtTheExplodedDirectory(t *testing.T) {
	base, project := fakeBase(t), fakeProject(t, "App")
	war := model.WarArtifact{SourcePath: project, DestName: "app.war", DeployMode: model.DeployExploded}

	if err := deployWar(base, war); err != nil {
		t.Fatalf("deployWar: %v", err)
	}
	raw, err := os.ReadFile(deploymentFor(base, war.DestName).descriptor)
	if err != nil {
		t.Fatalf("read descriptor: %v", err)
	}
	wantDoc := filepath.Join(project, "target", "App")
	if !strings.Contains(string(raw), escapeForXMLAttr(wantDoc)) {
		t.Errorf("descriptor does not point at %s:\n%s", wantDoc, raw)
	}
}

// Switching modes must leave exactly one deployment behind. Two would make
// Tomcat deploy the same context twice.
func TestSwitchingModesLeavesNoDuplicateDeployment(t *testing.T) {
	base, project := fakeBase(t), fakeProject(t, "App")
	war := model.WarArtifact{SourcePath: project, DestName: "app.war", DeployMode: model.DeployCopy}
	d := deploymentFor(base, war.DestName)

	if err := deployWar(base, war); err != nil {
		t.Fatalf("copy deploy: %v", err)
	}
	// Tomcat unpacks the copied war; simulate that leftover.
	if err := os.MkdirAll(d.unpackedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	war.DeployMode = model.DeployExploded
	if err := deployWar(base, war); err != nil {
		t.Fatalf("exploded deploy: %v", err)
	}
	if exists(d.warFile) || exists(d.unpackedDir) {
		t.Errorf("copy-mode leftovers survived the switch: war=%v dir=%v", exists(d.warFile), exists(d.unpackedDir))
	}
	if !exists(d.descriptor) {
		t.Error("descriptor missing after switching to exploded mode")
	}

	war.DeployMode = model.DeployCopy
	if err := deployWar(base, war); err != nil {
		t.Fatalf("copy deploy again: %v", err)
	}
	if exists(d.descriptor) {
		t.Error("descriptor survived the switch back to copy mode")
	}
	if !exists(d.warFile) {
		t.Error("war missing after switching back to copy mode")
	}
}

// The exploded build output must never be deleted by a deploy: it is the
// docBase Tomcat serves from.
func TestDeployNeverTouchesTheBuildOutput(t *testing.T) {
	base, project := fakeBase(t), fakeProject(t, "App")
	war := model.WarArtifact{SourcePath: project, DestName: "app.war", DeployMode: model.DeployExploded}

	if err := deployWar(base, war); err != nil {
		t.Fatalf("deployWar: %v", err)
	}
	for _, path := range []string{
		filepath.Join(project, "target", "App", "WEB-INF"),
		filepath.Join(project, "target", "App.war"),
	} {
		if !exists(path) {
			t.Errorf("deploy removed build output %s", path)
		}
	}
}

func TestClearKeepsWebappsDirectoryItself(t *testing.T) {
	base := fakeBase(t)
	if err := deploymentFor(base, "ROOT.war").clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if !exists(webappsDir(base)) {
		t.Fatal("clear() deleted the webapps directory")
	}
}

func TestFindExplodedInTarget(t *testing.T) {
	project := fakeProject(t, "App")
	got, err := findExplodedInTarget(project)
	if err != nil {
		t.Fatalf("findExplodedInTarget: %v", err)
	}
	if want := filepath.Join(project, "target", "App"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// A target/ with only bookkeeping directories has no exploded webapp.
	bare := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bare, "target", "maven-status"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := findExplodedInTarget(bare); err == nil {
		t.Error("findExplodedInTarget on a target/ with no webapp = nil error; want error")
	}
}

func TestPrepareInstanceSeedsIsolatedBase(t *testing.T) {
	install := t.TempDir()
	if err := os.MkdirAll(filepath.Join(install, "conf", "Catalina", "localhost"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(install, "conf", "server.xml"), []byte("<Server/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(install, "conf", "tomee.xml"), []byte("<tomee/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	// os.UserConfigDir reads these; keep the isolated base inside the test dir.
	home := t.TempDir()
	t.Setenv("APPDATA", home)
	t.Setenv("XDG_CONFIG_HOME", home)

	config := model.Config{TomEEPath: install, IsolatedBase: true}
	base, err := prepareInstance(config)
	if err != nil {
		t.Fatalf("prepareInstance: %v", err)
	}
	if base == install {
		t.Fatal("isolated base resolved to the installation directory")
	}
	for _, rel := range []string{
		filepath.Join("conf", "server.xml"),
		filepath.Join("conf", "tomee.xml"),
		"logs", "temp", "work", "webapps",
		filepath.Join("conf", "Catalina", "localhost"),
	} {
		if !exists(filepath.Join(base, rel)) {
			t.Errorf("isolated base is missing %s", rel)
		}
	}

	// Seeding happens once: a later edit in the base must survive.
	edited := filepath.Join(base, "conf", "server.xml")
	if err := os.WriteFile(edited, []byte("<Server port=\"9005\"/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareInstance(config); err != nil {
		t.Fatalf("second prepareInstance: %v", err)
	}
	raw, err := os.ReadFile(edited)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "9005") {
		t.Error("re-seeding overwrote an edited conf file")
	}
}

func TestPrepareInstanceLeavesInstallationAloneWhenNotIsolated(t *testing.T) {
	install := t.TempDir()
	base, err := prepareInstance(model.Config{TomEEPath: install})
	if err != nil {
		t.Fatalf("prepareInstance: %v", err)
	}
	if base != install {
		t.Errorf("base = %q, want the installation %q", base, install)
	}
	if !exists(contextDir(base)) {
		t.Error("conf/Catalina/localhost was not created")
	}
}

// escapeForXMLAttr mirrors the escaping etree applies, so the assertions above
// compare against what actually lands in the file for Windows paths.
func escapeForXMLAttr(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
