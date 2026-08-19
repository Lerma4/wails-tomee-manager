package service

import (
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidateProjectDir(t *testing.T) {
	abs := "/srv/app"
	if runtime.GOOS == "windows" {
		abs = `C:\srv\app`
	}

	ok := []struct{ in, want string }{
		{abs, abs},
		{abs + string(filepath.Separator), abs}, // trailing separator must be accepted
		{"  " + abs + "  ", abs},                // surrounding whitespace
		{filepath.Join(abs, "sub", ".."), abs},  // resolvable ".."
	}
	for _, c := range ok {
		got, err := validateProjectDir(c.in)
		if err != nil || got != c.want {
			t.Errorf("validateProjectDir(%q) = %q, %v; want %q, nil", c.in, got, err, c.want)
		}
	}

	for _, in := range []string{"", "   ", "relative/path"} {
		if _, err := validateProjectDir(in); err == nil {
			t.Errorf("validateProjectDir(%q) = nil error; want error", in)
		}
	}
}

func TestPortBusy(t *testing.T) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Skipf("cannot listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if !portBusy(port) {
		t.Errorf("portBusy(%d) = false while listening", port)
	}
	ln.Close()
	if portBusy(port) {
		t.Errorf("portBusy(%d) = true after close", port)
	}
}

func TestHTTPAlive(t *testing.T) {
	// 404 is the realistic case: TomEE with no root webapp. It still means "up".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	if !httpAlive(port) {
		t.Errorf("httpAlive(%d) = false while the server is serving 404", port)
	}
	srv.Close()
	if httpAlive(port) {
		t.Errorf("httpAlive(%d) = true after the server was closed", port)
	}
}
