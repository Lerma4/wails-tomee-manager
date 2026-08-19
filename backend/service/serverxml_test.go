package service

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
	"tomee-manager/backend/model"

	"github.com/beevik/etree"
)

// seedServerXML copies the real-world fixture into a throwaway CATALINA_BASE.
func seedServerXML(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	confDir := filepath.Join(base, "conf")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "server.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(confDir, "server.xml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return base
}

// connectorPorts maps each connector's identifying attribute to its port, so
// the assertions do not depend on document order.
func connectorPorts(t *testing.T, base string) map[string]string {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromFile(filepath.Join(base, "conf", "server.xml")); err != nil {
		t.Fatal(err)
	}
	ports := map[string]string{}
	for _, c := range doc.FindElements("//Connector") {
		switch {
		case c.SelectAttrValue("protocol", "") == "AJP/1.3":
			ports["ajp"] = c.SelectAttrValue("port", "")
		case c.SelectAttrValue("SSLEnabled", "") == "true":
			ports["https"] = c.SelectAttrValue("port", "")
		default:
			ports["http"] = c.SelectAttrValue("port", "")
		}
	}
	return ports
}

// Regression: the protocol is spelled "org.apache.coyote.http11.Http11NioProtocol",
// so a case-sensitive search for "HTTP" matched nothing and the configured HTTP
// port was silently never applied.
func TestUpdateServerXmlSetsTheHTTPPort(t *testing.T) {
	base := seedServerXML(t)

	config := model.Config{HTTPPort: 9090, ShutdownPort: 9005}
	if err := updateServerXml(base, config); err != nil {
		t.Fatalf("updateServerXml: %v", err)
	}

	ports := connectorPorts(t, base)
	if ports["http"] != "9090" {
		t.Errorf("http connector port = %q, want 9090", ports["http"])
	}
	if ports["https"] != "8443" {
		t.Errorf("https connector port = %q, want it left at 8443", ports["https"])
	}
	if ports["ajp"] != "8009" {
		t.Errorf("ajp connector port = %q, want it left at 8009", ports["ajp"])
	}

	doc := etree.NewDocument()
	if err := doc.ReadFromFile(filepath.Join(base, "conf", "server.xml")); err != nil {
		t.Fatal(err)
	}
	if got := doc.FindElement("//Server").SelectAttrValue("port", ""); got != "9005" {
		t.Errorf("shutdown port = %q, want 9005", got)
	}
}

// Rewriting twice must be stable: a commented-out connector must not be
// revived, and no connector may be duplicated.
func TestUpdateServerXmlIsIdempotent(t *testing.T) {
	base := seedServerXML(t)
	config := model.Config{HTTPPort: 9090, ShutdownPort: 9005}

	for i := 0; i < 2; i++ {
		if err := updateServerXml(base, config); err != nil {
			t.Fatalf("updateServerXml pass %d: %v", i+1, err)
		}
	}

	doc := etree.NewDocument()
	if err := doc.ReadFromFile(filepath.Join(base, "conf", "server.xml")); err != nil {
		t.Fatal(err)
	}
	if n := len(doc.FindElements("//Connector")); n != 3 {
		t.Errorf("got %d connectors, want the original 3 (a commented-out one was revived?)", n)
	}
	if ports := connectorPorts(t, base); ports["http"] != "9090" {
		t.Errorf("http connector port = %q after two passes, want 9090", ports["http"])
	}
}

func TestIsPlainHTTPConnector(t *testing.T) {
	cases := []struct {
		name string
		xml  string
		want bool
	}{
		{"nio http", `<Connector port="8080" protocol="org.apache.coyote.http11.Http11NioProtocol"/>`, true},
		{"literal http", `<Connector port="8080" protocol="HTTP/1.1"/>`, true},
		{"no protocol attribute", `<Connector port="8080"/>`, true},
		{"ssl", `<Connector port="8443" protocol="org.apache.coyote.http11.Http11Protocol" SSLEnabled="true" scheme="https" secure="true"/>`, false},
		{"scheme https only", `<Connector port="8443" scheme="https"/>`, false},
		{"ajp", `<Connector port="8009" protocol="AJP/1.3"/>`, false},
		{"ajp nio class", `<Connector port="8009" protocol="org.apache.coyote.ajp.AjpNioProtocol"/>`, false},
	}
	for _, c := range cases {
		doc := etree.NewDocument()
		if err := doc.ReadFromString(c.xml); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := isPlainHTTPConnector(doc.Root()); got != c.want {
			t.Errorf("isPlainHTTPConnector(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestWaitForPortFree(t *testing.T) {
	if err := waitForPortFree(0, time.Millisecond); err != nil {
		t.Errorf("port 0 means unconfigured, want nil; got %v", err)
	}

	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Skipf("cannot listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	if err := waitForPortFree(port, 300*time.Millisecond); err == nil {
		t.Error("waitForPortFree returned nil while the port was still held")
	}

	ln.Close()
	if err := waitForPortFree(port, 2*time.Second); err != nil {
		t.Errorf("waitForPortFree after close = %v, want nil", err)
	}
}
