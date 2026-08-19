package service

import (
	"bufio"
	"io"
	"regexp"
	"strings"
	"time"
)

// LogEntry is one logical log record: the header line, its level line, and any
// stack trace that follows, kept together so an error cannot get lost in the
// middle of a scrolling console.
type LogEntry struct {
	Level string `json:"level"` // ERROR | WARN | INFO | DEBUG
	Text  string `json:"text"`  // full record, newline-separated
}

// Log levels, in the shapes TomEE actually emits.
//
// TomEE 1.7 (Tomcat 7) formats records with java.util.logging's SimpleFormatter,
// which splits one record over two lines and *localises the level name*:
//
//	Jul 09, 2018 8:57:13 AM org.apache.tomee.jdbc.TomEEDataSourceCreator destroy
//	GRAVE: Unable to unregister JDBC pool with JMX
//	javax.management.InstanceNotFoundException: openejb.management:...
//		at com.sun.jmx.interceptor.DefaultMBeanServerInterceptor.getMBean(...)
//
// so the level lives on the *second* line and reads GRAVE / AVVERTENZA /
// INFORMAZIONI on an Italian JVM. Matching only "SEVERE" or "ERROR" finds
// nothing. Tomcat 8+ (OneLineFormatter) and log4j shapes are matched too, so
// this keeps working if the server or a deployed app changes format.
var (
	levelAlias = map[string]string{
		"GRAVE": "ERROR", "SEVERE": "ERROR", "SEVERO": "ERROR", "ERROR": "ERROR", "FATAL": "ERROR",
		"AVVERTENZA": "WARN", "WARNING": "WARN", "WARN": "WARN",
		"INFORMAZIONI": "INFO", "INFO": "INFO", "CONFIGURAZIONE": "INFO", "CONFIG": "INFO",
		"FINEST": "DEBUG", "FINER": "DEBUG", "FINE": "DEBUG", "DEBUG": "DEBUG", "TRACE": "DEBUG",
	}
	// Longest-first: the alternation must not match "INFO" inside "INFORMAZIONI".
	levelNames = `GRAVE|SEVERE|SEVERO|ERROR|FATAL|AVVERTENZA|WARNING|WARN|INFORMAZIONI|INFO|CONFIGURAZIONE|CONFIG|FINEST|FINER|FINE|DEBUG|TRACE`

	// "GRAVE: message" — SimpleFormatter's second line.
	levelLineRe = regexp.MustCompile(`^(` + levelNames + `)\s*:`)
	// "... [ERROR] ..." — logback/log4j bracketed level.
	bracketRe = regexp.MustCompile(`\[(` + levelNames + `)\]`)

	// "javax.management.InstanceNotFoundException: ..." opens a stack trace.
	throwableRe = regexp.MustCompile(`^[\w$]+(\.[\w$]+)+(Exception|Error|Throwable|Fault)(:|$)`)
	// "... 42 more" / "... 42 altri" closes a nested one.
	moreRe = regexp.MustCompile(`^\s*\.\.\.\s+\d+\s+\w+$`)
)

// maxEntryLines caps one record so a runaway stack trace cannot grow without
// bound. Deep traces exist, but 500 frames is already unreadable.
const maxEntryLines = 500

// positionalLevels are the level names safe to recognise as a bare word. The
// JUL-only names (FINE, CONFIG, ...) are left out on purpose: "FINE" is an
// ordinary Italian word and would mislabel application messages. Those names
// only ever appear followed by a colon, which levelLineRe already covers.
var positionalLevels = map[string]string{
	"GRAVE": "ERROR", "SEVERE": "ERROR", "SEVERO": "ERROR", "ERROR": "ERROR", "FATAL": "ERROR",
	"AVVERTENZA": "WARN", "WARNING": "WARN", "WARN": "WARN",
	"INFORMAZIONI": "INFO", "INFO": "INFO",
	"DEBUG": "DEBUG", "TRACE": "DEBUG",
}

// fieldLevel finds a level sitting as its own word near the start of a line.
// This covers the layouts that put the level after a timestamp or a channel:
//
//	19-Aug-2026 10:23:45.123 SEVERE [main] ...   (Tomcat 8+ OneLineFormatter)
//	2026-08-19 10:23:45,123 ERROR [main] ...     (log4j)
//	4603  dev  WARN   [http-nio-8080-exec-5] ... (OpenJPA, as configured here)
func fieldLevel(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return "", false
	}
	for i, f := range fields {
		if i >= 4 {
			break
		}
		if lvl, ok := positionalLevels[f]; ok {
			return lvl, true
		}
	}
	return "", false
}

// lineLevel returns the level tagged on a single line, if any.
func lineLevel(line string) (string, bool) {
	if m := levelLineRe.FindStringSubmatch(line); m != nil {
		return levelAlias[m[1]], true
	}
	if lvl, ok := fieldLevel(line); ok {
		return lvl, true
	}
	if m := bracketRe.FindStringSubmatch(line); m != nil {
		return levelAlias[m[1]], true
	}
	return "", false
}

// isContinuation reports whether line is unmistakably part of the record being
// built. Level lines are deliberately NOT handled here — they depend on parser
// state, see logParser.add. Everything unrecognised starts a new record: erring
// towards splitting keeps a stray pattern from swallowing the whole log into
// one entry.
func isContinuation(line string) bool {
	if strings.TrimSpace(line) == "" {
		return true // blank line inside a stack trace; trimmed off on flush
	}
	if line[0] == ' ' || line[0] == '\t' {
		return true // "\tat com.foo.Bar(Bar.java:42)"
	}
	if strings.HasPrefix(line, "Caused by:") || strings.HasPrefix(line, "Suppressed:") {
		return true
	}
	return moreRe.MatchString(line) || throwableRe.MatchString(line)
}

// stackTraceLevel returns ERROR for a record that carries a bare stack trace
// with no level tag — TomEE prints those on shutdown failures, and losing them
// is exactly the reported problem.
func stackTraceLevel(lines []string) string {
	for _, l := range lines {
		if throwableRe.MatchString(l) || strings.HasPrefix(strings.TrimLeft(l, " \t"), "at ") {
			return "ERROR"
		}
	}
	return "INFO"
}

type logParser struct {
	buf   []string
	level string // level of the record so far; "" until a level line shows up
}

// add feeds one line in, returning the previous record once this line is found
// to start a new one.
func (p *logParser) add(line string) (LogEntry, bool) {
	lvl, isLevel := lineLevel(line)
	if len(p.buf) > 0 {
		// A level line belongs to the SimpleFormatter header buffered above it,
		// but only while the record has no level of its own — otherwise two
		// consecutive records would be glued together.
		if isContinuation(line) || (isLevel && p.level == "") {
			p.buf = append(p.buf, line)
			if isLevel && p.level == "" {
				p.level = lvl
			}
			if len(p.buf) >= maxEntryLines {
				return p.flush()
			}
			return LogEntry{}, false
		}
	}
	entry, ok := p.flush()
	p.buf = append(p.buf, line)
	if isLevel {
		p.level = lvl
	}
	return entry, ok
}

// flush closes the record in progress. Reports false when there is nothing but
// blank lines to emit.
func (p *logParser) flush() (LogEntry, bool) {
	lines, level := p.buf, p.level
	p.buf, p.level = nil, ""
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return LogEntry{}, false
	}
	if level == "" {
		level = stackTraceLevel(lines)
	}
	return LogEntry{Level: level, Text: strings.Join(lines, "\n")}, true
}

// entryFlushDelay bounds how long a record waits for continuation lines that
// may never come. A JUL record and its stack trace are written in one burst, so
// a buffer idle this long is complete — and the last line before a long pause
// still reaches the UI promptly.
const entryFlushDelay = 250 * time.Millisecond

// streamEntries reads lines from r and calls emit once per complete record.
// It returns when r reaches EOF.
func streamEntries(r io.Reader, emit func(LogEntry)) {
	lines := make(chan string, 512)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(r)
		// TomEE logs generated REST route tables in very wide lines; the 64KB
		// default would abort the whole scan on one of them.
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()

	p := &logParser{}
	timer := time.NewTimer(entryFlushDelay)
	defer timer.Stop()
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				if e, has := p.flush(); has {
					emit(e)
				}
				return
			}
			if e, has := p.add(line); has {
				emit(e)
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(entryFlushDelay)
		case <-timer.C:
			if e, has := p.flush(); has {
				emit(e)
			}
			timer.Reset(entryFlushDelay)
		}
	}
}
