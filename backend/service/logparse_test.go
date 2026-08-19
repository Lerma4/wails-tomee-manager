package service

import (
	"os"
	"strings"
	"testing"
)

// collect runs every line through the parser and returns the finished records.
func collect(lines []string) []LogEntry {
	p := &logParser{}
	var out []LogEntry
	for _, l := range lines {
		if e, ok := p.add(l); ok {
			out = append(out, e)
		}
	}
	if e, ok := p.flush(); ok {
		out = append(out, e)
	}
	return out
}

func TestLineLevel(t *testing.T) {
	cases := []struct{ line, want string }{
		// TomEE 1.7 / SimpleFormatter on an Italian JVM — the reported blind spot.
		{"GRAVE: Unable to unregister JDBC pool with JMX", "ERROR"},
		{"AVVERTENZA: Property \"maxTotal\" not supported", "WARN"},
		{"INFORMAZIONI: Server startup in 5366 ms", "INFO"},
		{"SEVERE: boom", "ERROR"},
		{"WARNING: careful", "WARN"},
		{"INFO: hello", "INFO"},
		{"FINE: chatty", "DEBUG"},
		// Tomcat 8+ OneLineFormatter and log4j, in case the server or an app changes.
		{"19-Aug-2026 10:23:45.123 SEVERE [main] org.apache.catalina.core.X boom", "ERROR"},
		{"2026-08-19 10:23:45,123 ERROR [main] com.foo.Bar - boom", "ERROR"},
		{"2026-08-19 10:23:45,123 DEBUG [main] com.foo.Bar - noise", "DEBUG"},
		{"something [WARN] mid-line", "WARN"},
		// A level sitting as a bare word, as OpenJPA and pipe-delimited app
		// layouts print it.
		{"4603  dev  WARN   [http-nio-8080-exec-5] openjpa.Enhance - transform failed", "WARN"},
		{"INFO  | http-bio-8443-exec-2 | Keycloak integration is disabled", "INFO"},
	}
	for _, c := range cases {
		got, ok := lineLevel(c.line)
		if !ok || got != c.want {
			t.Errorf("lineLevel(%q) = %q,%v; want %q,true", c.line, got, ok, c.want)
		}
	}

	// "INFORMAZIONI" must not be read as "INFO" plus junk, and a header line
	// carries no level of its own.
	for _, line := range []string{
		"Jul 09, 2018 8:57:13 AM org.apache.openejb.assembler.classic.Assembler destroyResource",
		"\tat com.sun.jmx.interceptor.DefaultMBeanServerInterceptor.getMBean(X.java:1095)",
		"Using CATALINA_BASE:   \"C:\tomee\"",
		"ELABORAZIONE FINE senza errori", // "FINE" is an Italian word, not a JUL level here
	} {
		if got, ok := lineLevel(line); ok {
			t.Errorf("lineLevel(%q) = %q,true; want no level", line, got)
		}
	}
}

func TestSimpleFormatterRecordKeepsHeaderAndStackTogether(t *testing.T) {
	got := collect([]string{
		"Jul 09, 2018 8:57:13 AM org.apache.tomee.jdbc.TomEEDataSourceCreator internalJMXUnregister",
		"GRAVE: Unable to unregister JDBC pool with JMX",
		"javax.management.InstanceNotFoundException: openejb.management:DataSource=sisenvDB",
		"\tat com.sun.jmx.interceptor.DefaultMBeanServerInterceptor.getMBean(X.java:1095)",
		"Caused by: java.lang.NullPointerException",
		"\tat com.foo.Bar.baz(Bar.java:12)",
		"\t... 42 more",
	})

	if len(got) != 1 {
		t.Fatalf("got %d records, want 1: %+v", len(got), got)
	}
	if got[0].Level != "ERROR" {
		t.Errorf("level = %q, want ERROR", got[0].Level)
	}
	// The whole thing must survive as one searchable block, header included.
	for _, want := range []string{"TomEEDataSourceCreator", "GRAVE:", "Caused by:", "... 42 more"} {
		if !strings.Contains(got[0].Text, want) {
			t.Errorf("record text is missing %q:\n%s", want, got[0].Text)
		}
	}
}

func TestConsecutiveRecordsDoNotMerge(t *testing.T) {
	got := collect([]string{
		"Jul 09, 2018 8:57:13 AM org.apache.openejb.assembler.classic.Assembler destroyResource",
		"INFORMAZIONI: Closing DataSource: sisenvNonJTADatabase",
		"Jul 09, 2018 8:57:13 AM org.apache.tomee.jdbc.TomEEDataSourceCreator internalJMXUnregister",
		"GRAVE: Unable to unregister JDBC pool with JMX",
	})
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2: %+v", len(got), got)
	}
	if got[0].Level != "INFO" || got[1].Level != "ERROR" {
		t.Errorf("levels = %q,%q; want INFO,ERROR", got[0].Level, got[1].Level)
	}
}

// A level line with no header above it must still start its own record rather
// than being swallowed by the previous one.
func TestBareLevelLineAfterCompleteRecordStartsNewRecord(t *testing.T) {
	got := collect([]string{
		"Jul 09, 2018 8:53:41 AM org.apache.catalina.startup.SetAllPropertiesRule begin",
		"AVVERTENZA: keystorefile did not find a matching property",
		"GRAVE: Unable to unregister JDBC pool with JMX",
	})
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2: %+v", len(got), got)
	}
	if got[0].Level != "WARN" || got[1].Level != "ERROR" {
		t.Errorf("levels = %q,%q; want WARN,ERROR", got[0].Level, got[1].Level)
	}
}

func TestUntaggedStackTraceIsAnError(t *testing.T) {
	got := collect([]string{
		"java.lang.IllegalStateException: no transaction",
		"\tat com.foo.Bar.baz(Bar.java:12)",
	})
	if len(got) != 1 || got[0].Level != "ERROR" {
		t.Fatalf("got %+v; want a single ERROR record", got)
	}
}

func TestBlankLinesProduceNoRecords(t *testing.T) {
	if got := collect([]string{"", "   ", ""}); len(got) != 0 {
		t.Errorf("got %+v; want no records", got)
	}
}

func TestRunawayStackTraceIsCapped(t *testing.T) {
	lines := []string{"GRAVE: boom"}
	for i := 0; i < maxEntryLines*2; i++ {
		lines = append(lines, "\tat com.foo.Bar.baz(Bar.java:1)")
	}
	got := collect(lines)
	if len(got) < 2 {
		t.Fatalf("got %d records, want the trace split across several", len(got))
	}
	for _, e := range got {
		if n := strings.Count(e.Text, "\n") + 1; n > maxEntryLines {
			t.Errorf("record has %d lines, over the %d cap", n, maxEntryLines)
		}
	}
}

// The fixture is a verbatim excerpt of a real catalina.out from a TomEE 1.7.3
// running on an Italian JVM.
func TestParsesRealCatalinaOutput(t *testing.T) {
	raw, err := os.ReadFile("testdata/catalina-sample.log")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	got := collect(strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n"))

	counts := map[string]int{}
	for _, e := range got {
		counts[e.Level]++
	}
	if counts["ERROR"] != 2 {
		t.Errorf("ERROR records = %d, want 2 (both GRAVE): %v", counts["ERROR"], counts)
	}
	if counts["WARN"] != 1 {
		t.Errorf("WARN records = %d, want 1 (the AVVERTENZA): %v", counts["WARN"], counts)
	}
	if counts["ERROR"]+counts["WARN"]+counts["INFO"] != len(got) {
		t.Errorf("unexpected levels: %v", counts)
	}

	// The stack trace must ride along with its GRAVE line, not float free.
	var withTrace int
	for _, e := range got {
		if strings.Contains(e.Text, "InstanceNotFoundException") {
			withTrace++
			if e.Level != "ERROR" {
				t.Errorf("stack trace record has level %q, want ERROR", e.Level)
			}
			if !strings.Contains(e.Text, "GRAVE: Unable to unregister") {
				t.Errorf("stack trace was split from its GRAVE line:\n%s", e.Text)
			}
		}
	}
	if withTrace != 1 {
		t.Errorf("stack trace spread over %d records, want 1", withTrace)
	}
}

func TestStreamEntriesEmitsEveryRecord(t *testing.T) {
	raw, err := os.ReadFile("testdata/catalina-sample.log")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var got []LogEntry
	streamEntries(strings.NewReader(string(raw)), func(e LogEntry) { got = append(got, e) })

	if len(got) == 0 {
		t.Fatal("streamEntries emitted nothing")
	}
	var errs int
	for _, e := range got {
		if e.Level == "ERROR" {
			errs++
		}
	}
	if errs != 2 {
		t.Errorf("ERROR records = %d, want 2", errs)
	}
}
