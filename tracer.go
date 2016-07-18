// +build linux freebsd


package bcd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type pipes struct {
	stdin  io.Reader
	stderr io.Writer
}

type connection struct {
	endpoint string
	client http.Client
	unlink bool
}

type BTTracer struct {
	// Path to the tracer to invoke.
	path string

	// Generic options to pass to the tracer.
	options []string

	// Prefix for key-value options.
	kvp string

	// Delimeter between key and value for key-value options.
	kvd string

	// Channel which receives signal notifications.
	sigs chan os.Signal

	// The set of signals the tracer will monitor.
	ss []os.Signal

	// The pipes to use for tracer I/O.
	p pipes

	// Protects tracer state modification.
	m sync.RWMutex

	// Logs tracer execution status messages.
	logger Log

	// Default trace options to use if none are specified to bcd.Trace().
	defaultTraceOptions TraceOptions

	// XXX
	uploader connection
}

type defaultLogger struct {
	logger *log.Logger
	level  LogPriority
}

func (d *defaultLogger) Logf(level LogPriority, format string, v ...interface{}) {
	if (d.level & level) == 0 {
		return
	}

	d.logger.Printf(format, v...)
}

func (d *defaultLogger) SetLogLevel(level LogPriority) {
	d.level = level
}

// Returns a new object implementing the bcd.Tracer and bcd.TracerSig interfaces
// using the Backtrace debugging platform. Currently, only Linux and FreeBSD
// are supported.
//
// Relevant default values:
//
// Path: /opt/backtrace/bin/ptrace
//
// Signal set: ABRT, FPE, SEGV, ILL, BUS, INT. Note: Go converts BUS, FPE, and
// SEGV arising from process execution into run-time panics, which cannot be
// handled by signal handlers. These signals are caught went sent from
// os.Process.Kill or similar.
//
// System goroutines (i.e. those started and used by the Go runtime) are
// excluded unless the includeSystemGs parameter is true.
//
// The default logger prints to stderr.
//
// DefaultTraceOptions:  
// Faulted: true  
// CallerOnly: false  
// ErrClassification: true  
// Timeout: 120s
func New(includeSystemGs bool) *BTTracer {
	moduleOpt := "--module=go:enable,true"
	if !includeSystemGs {
		moduleOpt += ",filter,user"
	}

	return &BTTracer{
		path: "/opt/backtrace/bin/ptrace",
		kvp:  "--kv",
		kvd:  ":",
		options: []string{
			"--load=",
			moduleOpt,
			"--faulted",
			strconv.Itoa(os.Getpid())},
		ss: []os.Signal{
			syscall.SIGABRT,
			syscall.SIGFPE,
			syscall.SIGSEGV,
			syscall.SIGILL,
			syscall.SIGBUS,
			syscall.SIGINT},
		logger: &defaultLogger{
			logger: log.New(os.Stderr, "[bcd] ", log.LstdFlags),
			level: LogError},
		defaultTraceOptions: TraceOptions{
			Faulted:           true,
			CallerOnly:        false,
			ErrClassification: true,
			Timeout:           time.Second * 120}}
}

type PutOptions struct {
	// XXX
	Protocol string

	// XXX
	Port uint

	// XXX
	Unlink bool
}

// XXX
func (t *BTTracer) EnablePut(host, token string, options PutOptions) error {
	if host == "" || token == "" {
		return errors.New("Host and token must be non-empty")
	}

	if options.Protocol == "" {
		options.Protocol = "https"
	}

	if options.Port == 0 {
		options.Port = 6098
	}

	t.uploader.endpoint = fmt.Sprintf("%s://%s:%d/post?token=%s",
		options.Protocol,
		host,
		options.Port,
		token);
	t.uploader.unlink = options.Unlink

	t.Logf(LogDebug, "Put enabled (endpoint: %s, unlink: %v)\n",
		t.uploader.endpoint,
		t.uploader.unlink)

	return nil
}

// XXX
func (t *BTTracer) PutEnabled() bool {
	return t.uploader.endpoint != ""
}

// XXX
func (t *BTTracer) Put(snapshot []byte) error {
	end := bytes.IndexByte(snapshot, 0)
	if end == -1 {
		end = len(snapshot)
	}
	path := strings.TrimSpace(string(snapshot[:end]))

	t.Logf(LogDebug, "Attempting to upload snapshot %s...\n", path)

	body, err := os.Open(path)
	if err != nil {
		return err
	}

	// The file is automatically closed by the Post request after
	// completion.

	resp, err := t.uploader.client.Post(
		t.uploader.endpoint,
		"application/octet-stream",
		body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to upload: %s", resp.Status)
	}

	if t.uploader.unlink {
		t.Logf(LogDebug, "Unlinking snapshot...\n")

		if err := os.Remove(path); err != nil {
			t.Logf(LogWarning,
				"Failed to unlink snapshot: %v\n",
				err)

			// This does not mean the put itself failed,
			// so we don't return this error here.
		} else {
			t.Logf(LogDebug, "Unlinked snapshot\n")
		}
	}

	t.Logf(LogDebug, "Uploaded snapshot\n")

	return nil
}

// Sets the executable path for the tracer.
func (t *BTTracer) SetPath(path string) {
	t.m.Lock()
	defer t.m.Unlock()

	t.path = path
}

// XXX SetPath -> SetTracerPath
// XXX SetOutputPath(path string)

// Sets the input and output pipes for the tracer.
// Stdout is not redirected; it is instead passed to the
// tracer's Put command.
func (t *BTTracer) SetPipes(stdin io.Reader, stderr io.Writer) {
	t.m.Lock()
	defer t.m.Unlock()

	t.p.stdin = stdin
	t.p.stderr = stderr
}

// Sets the logger for the tracer.
func (t *BTTracer) SetLogger(logger Log) {
	t.logger = logger
}

// See bcd.Tracer.AddOptions().
func (t *BTTracer) AddOptions(options []string, v ...string) []string {
	if options != nil {
		return append(options, v...)
	}

	t.m.Lock()
	defer t.m.Unlock()

	t.options = append(t.options, v...)
	return nil
}

// See bcd.Tracer.AddKV().
func (t *BTTracer) AddKV(options []string, key, val string) []string {
	return t.AddOptions(options, t.kvp, key+t.kvd+val)
}

// See bcd.Tracer.AddThreadFilter().
func (t *BTTracer) AddThreadFilter(options []string, tid int) []string {
	return t.AddOptions(options, "--thread", strconv.Itoa(tid))
}

// See bcd.Tracer.AddFaultedThread().
func (t *BTTracer) AddFaultedThread(options []string, tid int) []string {
	return t.AddOptions(options, "--fault-thread", strconv.Itoa(tid))
}

// See bcd.Tracer.AddClassifier().
func (t *BTTracer) AddClassifier(options []string, classifier string) []string {
	return t.AddOptions(options, "--classifier", classifier)
}

// See bcd.Tracer.Options().
func (t *BTTracer) Options() []string {
	t.m.RLock()
	defer t.m.RUnlock()

	return append([]string(nil), t.options...)
}

// See bcd.Tracer.ClearOptions().
func (t *BTTracer) ClearOptions() {
	t.m.Lock()
	defer t.m.Unlock()

	t.options = nil
}

// See bcd.Tracer.DefaultTraceOptions().
func (t *BTTracer) DefaultTraceOptions() *TraceOptions {
	return &t.defaultTraceOptions
}

// See bcd.Tracer.Finalize().
func (t *BTTracer) Finalize(options []string) *exec.Cmd {
	t.m.RLock()
	defer t.m.RUnlock()

	tracer := exec.Command(t.path, options...)
	tracer.Stdin = t.p.stdin
	tracer.Stderr = t.p.stderr

	t.Logf(LogDebug, "Command: %v\n", tracer)

	return tracer
}

func (t *BTTracer) Logf(level LogPriority, format string, v ...interface{}) {
	t.m.RLock()
	defer t.m.RUnlock()

	t.logger.Logf(level, format, v...)
}

func (t *BTTracer) SetLogLevel(level LogPriority) {
	t.m.RLock()
	defer t.m.RUnlock()

	t.logger.SetLogLevel(level)
}

func (t *BTTracer) String() string {
	t.m.RLock()
	defer t.m.RUnlock()

	return fmt.Sprintf("Path: %s, Options: %v", t.path, t.options)
}

// See bcd.TracerSig.SetSigset().
func (t *BTTracer) SetSigset(sigs ...os.Signal) {
	t.ss = append([]os.Signal(nil), sigs...)
}

// See bcd.TracerSig.Sigset().
func (t *BTTracer) Sigset() []os.Signal {
	return append([]os.Signal(nil), t.ss...)
}

// See bcd.TracerSig.SetSigchan().
func (t *BTTracer) SetSigchan(sc chan os.Signal) {
	t.sigs = sc
}

// See bcd.TracerSig.Sigchan().
func (t *BTTracer) Sigchan() chan os.Signal {
	return t.sigs
}
