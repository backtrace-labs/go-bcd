// +build linux freebsd

package bcd

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

type pipes struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
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

// Sets the executable path for the tracer.
func (t *BTTracer) SetPath(path string) {
	t.m.Lock()
	defer t.m.Unlock()

	t.path = path
}

// Sets the input and output pipes for the tracer.
func (t *BTTracer) SetPipes(stdin io.Reader, stdout, stderr io.Writer) {
	t.m.Lock()
	defer t.m.Unlock()

	t.p.stdin = stdin
	t.p.stdout = stdout
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
	tracer.Stdout = t.p.stdout
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
