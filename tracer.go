// +build linux freebsd

package bcd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
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

type uploader struct {
	endpoint string
	client   http.Client
	options  PutOptions
}

type BTTracer struct {
	// Path to the tracer to invoke.
	cmd string

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

	// The connection information and options used during Put operations.
	put uploader
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
// Signal set: ABRT, FPE, SEGV, ILL, BUS. Note: Go converts BUS, FPE, and
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
//
// Faulted: true
//
// CallerOnly: false
//
// ErrClassification: true
//
// Timeout: 120s
func New(includeSystemGs bool) *BTTracer {
	moduleOpt := "--module=go:enable,true"
	if !includeSystemGs {
		moduleOpt += ",filter,user"
	}

	return &BTTracer{
		cmd: "/opt/backtrace/bin/ptrace",
		kvp: "--kv",
		kvd: ":",
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
			syscall.SIGBUS},
		logger: &defaultLogger{
			logger: log.New(os.Stderr, "[bcd] ", log.LstdFlags),
			level:  LogError},
		defaultTraceOptions: TraceOptions{
			Faulted:           true,
			CallerOnly:        false,
			ErrClassification: true,
			Timeout:           time.Second * 120}}
}

const (
	defaultCoronerScheme = "https"
	defaultCoronerPort   = "6098"
)

type PutOptions struct {
	// Set to true if tracer remnants (i.e. generated snapshot files)
	// should be unlinked from the filesystem after successful puts.
	Unlink bool
}

// Enables uploading of the generated snapshot file to a remote Backtrace
// coronerd object store.
//
// endpoint: The URL of the server. It must be a valid HTTP endpoint as
// according to url.Parse() (which is based on RFC 3986). The default scheme
// and port are https and 6098, respectively, and are used if left unspecified.
//
// token: The hash associated with the coronerd project to which this
// application belongs; see
// https://documentation.backtrace.io/coronerd_setup/#authentication-tokens
// for more details.
//
// options: Modifies behavior of the Put action; see PutOptions documentation
// for more details.
func (t *BTTracer) EnablePut(endpoint, token string, options PutOptions) error {
	if endpoint == "" || token == "" {
		return errors.New("Endpoint must be non-empty")
	}

	url, err := url.Parse(endpoint)
	if err != nil {
		return err
	}

	// Endpoints without the scheme prefix (or at the very least a '//`
	// prefix) are interpreted as remote server paths. Handle the
	// (unlikely) case of an unspecified scheme. We won't allow other
	// cases, like a port specified without a scheme, though, as per
	// RFC 3986.
	if url.Host == "" {
		if url.Path == "" {
			return errors.New("invalid URL specification: host " +
				"or path must be non-empty")
		}

		url.Host = url.Path
	}

	if url.Scheme == "" {
		url.Scheme = defaultCoronerScheme
	}

	if strings.IndexAny(url.Host, ":") == -1 {
		url.Host += ":" + defaultCoronerPort
	}

	url.Path = "post"
	url.RawQuery = fmt.Sprintf("token=%s", token)

	t.put.endpoint = url.String()
	t.put.options = options

	t.Logf(LogDebug, "Put enabled (endpoint: %s, unlink: %v)\n",
		t.put.endpoint,
		t.put.options.Unlink)

	return nil
}

// See bcd.Tracer.PutEnabled().
func (t *BTTracer) PutEnabled() bool {
	return t.put.endpoint != ""
}

// See bcd.Tracer.Put().
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

	resp, err := t.put.client.Post(
		t.put.endpoint,
		"application/octet-stream",
		body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to upload: %s", resp.Status)
	}

	if t.put.options.Unlink {
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
func (t *BTTracer) SetTracerPath(path string) {
	t.m.Lock()
	defer t.m.Unlock()

	t.cmd = path
}

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

	tracer := exec.Command(t.cmd, options...)
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

	return fmt.Sprintf("Command: %s, Options: %v", t.cmd, t.options)
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
