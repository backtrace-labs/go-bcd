// Package bcd provides integration with out-of-process tracers. Using the
// provided Tracer interface, applications may invoke tracer execution on
// demand. Panic and signal handling integrations are provided.
//
// The Tracer interface is generic and will support any out-of-process tracer
// implementing it. A default Tracer implementation, which uses the
// Backtrace I/O platform, is provided.
package bcd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"reflect"
	"runtime"
	"sync"
	"time"
)

var (
	// Only one tracer should be running on a process at any time; this is
	// global to all created tracers.
	traceLock chan struct{}

	// Configuration applicable to all tracer invocations.
	state globalState
)

type globalState struct {
	c GlobalConfig
	m sync.RWMutex
}

type GlobalConfig struct {
	// If the tracer's timeout expires and the tracer cannot be killed,
	// generate a run-time panic.
	// Defaults to true.
	PanicOnKillFailure bool

	// Upon receipt of a signal and execution of the tracer, re-sends the
	// signal to the default Go signal handler for the signal and stops
	// listening for the signal.
	// Note: this will call signal.Reset(signal) on the received signal,
	// which undoes the effect of any signal.Notify() calls for the signal.
	// Defaults to true.
	ResendSignal bool

	// Length of time to wait after completion of a tracer's
	// execution before allowing the next tracer to run.
	// Defaults to 3 seconds.
	RateLimit time.Duration

	// Wait for the Tracer to finish uploading its results (instead of
	// asynchronously uploading in a new goroutine) before returning
	// from bcd.Trace().
	// Defaults to true.
	SynchronousPut bool
}

func init() {
	// Tracer execution timeouts are supported, which is why we use a
	// channel here instead of sync.Mutex.
	traceLock = make(chan struct{}, 1)
	traceLock <- struct{}{}

	state = globalState{
		c: GlobalConfig{
			PanicOnKillFailure: true,
			ResendSignal:       true,
			RateLimit:          time.Second * 3,
			SynchronousPut:     true}}
}

// Update global Tracer configuration.
func UpdateConfig(c *GlobalConfig) {
	state.m.Lock()
	defer state.m.Unlock()

	state.c = *c
}

// A generic out-of-process tracer interface.
// The methods in this interface are expected to be goroutine safe; multiple
// trace requests (which ultimately call into these methods) from different
// goroutines may run concurrently.
type Tracer interface {
	// Store the options provided by v.
	//
	// If the options slice is non-nil, the provided options should be
	// stored in it; otherwise, the options are added to the Tracer's
	// base set of options.
	// Returns the final options slice if the provided options slice is
	// non-nil.
	AddOptions(options []string, v ...string) []string

	// Add a key-value attribute.
	//
	// See AddOptions for rules regarding the specified options slice and
	// the return value.
	AddKV(options []string, key, val string) []string

	// Add a thread filter option using the specified tid. If any thread
	// filter options are added, all non-matching threads and goroutines
	// are expected to be excluded from the generated snapshot.
	//
	// See AddOptions for rules regarding the specified options slice and
	// the return value.
	AddThreadFilter(options []string, tid int) []string

	// Add a faulted thread option using the specified tid. Threads and
	// goroutines matching any faulted thread options are marked as faulted
	// and subject to analysis and grouping.
	//
	// See AddOptions for rules regarding the specified options slice and
	// the return value.
	AddFaultedThread(options []string, tid int) []string

	// Add a classification to the generated snapshot.
	//
	// See AddOptions for rules regarding the specified options slice and
	// the return value.
	AddClassifier(options []string, classifier string) []string

	// Returns a copy of the base set of options for the Tracer.
	Options() []string

	// Clears the base set of options for the Tracer.
	ClearOptions()

	// Returns the default TraceOptions used in bcd.Trace() if an override
	// is not specified as an argument to it.
	DefaultTraceOptions() *TraceOptions

	// Accepts a final set of options and returns a Command object
	// representing a tracer that is ready to run. This will be executed
	// on the current process.
	Finalize(options []string) *exec.Cmd

	// Determines when and to what the Tracer will log.
	Log

	// String representation of a Tracer.
	fmt.Stringer

	// Returns whether the tracer should upload its results to a remote
	// server after successful tracer execution.
	PutEnabled() bool

	// Uploads Tracer results given by the snapshot argument, which is
	// the stdout of the Tracer process, to the configured remote server.
	Put(snapshot []byte) error
}

// Options determining actions taken during Tracer execution.
type TraceOptions struct {
	// If true, the calling thread/goroutine will be marked as faulted
	// (i.e. the cause of the error or trace request).
	//
	// This is a Linux-specific option; it results in a noop on other
	// systems.
	Faulted bool

	// If true, only the calling thread/goroutine will be traced; all others
	// will be excluded from the generated snapshot.
	//
	// This is a Linux-specific option; it results in a noop on other
	// systems.
	CallerOnly bool

	// If true and a non-nil error object is passed to bcd.Trace(), a
	// classifier will be added based on the specified error's type.
	ErrClassification bool

	// If non-nil, all contained strings will be added as classifiers to
	// the generated snapshot.
	Classifications []string

	// Amount of time to wait for the tracer to finish execution.
	// If 0 is specified, Tracer.DefaultTraceOptions()'s timeout will be
	// used. If <0 is specified, no timeout will be used; the Tracer command
	// will run until it exits.
	Timeout time.Duration
}

type Log interface {
	// Logs the specified message if the specified log level is enabled.
	Logf(level LogPriority, format string, v ...interface{})

	// Sets the log level to the specified bitmask of LogPriorities; all
	// priorities excluded from the mask are ignored.
	SetLogLevel(level LogPriority)
}

type LogPriority int

const (
	LogDebug = 1 << iota
	LogWarning
	LogError
	LogMax = (1 << iota) - 1
)

// This is a superset of the generic Tracer interface for those that wish
// to support signal handling. The methods unique to this interface are
// not expected to be goroutine-safe.
type TracerSig interface {
	Tracer

	// Sets the desired set of signals for which to invoke the Tracer upon
	// receipt of the signal.
	SetSigset(sigs ...os.Signal)

	// Returns the desired signal set.
	Sigset() []os.Signal

	// Sets the channel through which the Tracer will respond to signals.
	SetSigchan(sc chan os.Signal)

	// Returns the channel through which the Tracer will respond to signals.
	Sigchan() chan os.Signal
}

// Create a unique error to pass to a Trace request.
type signalError struct {
	s os.Signal
}

func (s *signalError) Error() string {
	return s.s.String()
}

// Registers a signal handler to execute the specified Tracer upon receipt of
// any signal in the set specified by TracerSig.Sigset().
// If the GlobalConfiguration value ResendSignal is true, then when a signal is
// received through this handler, all handlers for that signal will be reset
// with signal.Reset(s) after tracer execution completes. The signal will then
// be resent to the default Go handler for that signal.
func Register(t TracerSig) {
	ss := t.Sigset()
	if ss == nil || len(ss) == 0 {
		t.Logf(LogError, "Failed to register signal handler: empty "+
			"sigset\n")
		return
	}

	c := t.Sigchan()
	if c != nil {
		unregisterInternal(t, c)
	}

	c = make(chan os.Signal, len(ss))
	t.SetSigchan(c)

	signal.Notify(c, ss...)

	t.Logf(LogDebug, "Registered tracer %s (signal set: %v)\n", t, ss)

	state.m.RLock()
	rs := state.c.ResendSignal
	state.m.RUnlock()

	go func(t TracerSig) {
		for s := range c {
			t.Logf(LogDebug, "Received %v; executing tracer\n", s)

			Trace(t, &signalError{s}, nil)

			if !rs {
				continue
			}

			t.Logf(LogDebug, "Resending %v to default handler\n", s)

			// Re-handle the signal with the default Go behavior.
			signal.Reset(s)
			p, err := os.FindProcess(os.Getpid())
			if err != nil {
				t.Logf(LogError, "Failed to resend signal: "+
					"cannot find process object")
				return
			}

			p.Signal(s)
		}

		t.Logf(LogDebug, "Signal channel closed; exiting goroutine\n")
	}(t)

	return
}

func unregisterInternal(t TracerSig, c chan os.Signal) {
	t.Logf(LogDebug, "Stopping signal channel...\n")
	signal.Stop(c)

	t.Logf(LogDebug, "Closing signal channel...\n")
	close(c)

	t.SetSigchan(nil)
	t.Logf(LogDebug, "Tracer unregistered\n")
}

// Stops the specified TracerSig from handling any signals it was previously
// registered to handle via bcd.Register().
func Unregister(t TracerSig) {
	c := t.Sigchan()
	if c == nil {
		return
	}

	unregisterInternal(t, c)
}

func traceUnlockRL(t Tracer, rl time.Duration) {
	t.Logf(LogDebug, "Waiting for ratelimit (%v)\n", rl)
	<-time.After(rl)
	t.Logf(LogDebug, "Unlocking traceLock\n")
	traceLock <- struct{}{}
}

type tracerResult struct {
	stdOut []byte
	err    error
}

// Executes the specified Tracer on the current process.
//
// If e is non-nil, it will be used to augment the trace according to the
// TraceOptions.
// If traceOptions is non-nil, it will be used instead of the Tracer's
// DefaultTraceOptions(). See TraceOptions for details on the various options.
//
// This is goroutine-safe; multiple goroutines may share the same Tracer and
// execute Trace() concurrently. Only one tracer will be allowed to run at
// any point; others will wait to acquire resources (locks) or timeout (if
// timeouts are not disabled). Trace execution will be rate-limited according
// to the GlobalConfig settings.
//
// This may also be called in a new goroutine via go Trace(...). In that case,
// ensure TraceOptions.CallerOnly is false (you will likely also want to set
// TraceOptions.Faulted to false); otherwise, only the newly spawned goroutine
// will be traced.
//
// Output of specific Tracer execution depends on the implementation; most
// Tracers will have options for specifying output paths.
func Trace(t Tracer, e error, traceOptions *TraceOptions) (err error) {
	if traceOptions == nil {
		traceOptions = t.DefaultTraceOptions()
	}

	// If no timeouts are specified, the timeout channel will block
	// forever (i.e. it will return only after the tracer exits).
	// We create the timer first to account for the work below, but
	// we won't wrap setup in a timeout as it's unlikely to be
	// a bottleneck.
	var timeout <-chan time.Time

	if traceOptions.Timeout == 0 {
		to := t.DefaultTraceOptions().Timeout
		timeout = time.After(to)
		t.Logf(LogDebug, "Tracer timeout: %v\n", to)
	} else if traceOptions.Timeout > 0 {
		timeout = time.After(traceOptions.Timeout)
		t.Logf(LogDebug, "Tracer timeout: %v\n", traceOptions.Timeout)
	}

	// We create a new options slice to avoid modifying the base
	// set of tracer options just for this particular trace
	// invocation.
	options := t.Options()

	// If the caller has requested a trace with thread-specific options,
	// then add the relevant thread specifications to the options list.
	if traceOptions.CallerOnly || traceOptions.Faulted {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		if tid, err := gettid(); err == nil {
			t.Logf(LogDebug, "Retrieved tid: %v\n", tid)

			if traceOptions.CallerOnly {
				options = t.AddThreadFilter(options, tid)
			}

			if traceOptions.Faulted {
				options = t.AddFaultedThread(options, tid)
			}
		} else {
			t.Logf(LogWarning, "Failed to retrieve tid: %v\n", err)
		}
	}

	if e != nil {
		options = t.AddKV(options, "error", e.Error())
		if traceOptions.ErrClassification {
			options = t.AddClassifier(options,
				reflect.TypeOf(e).String())
		}
	}

	for _, c := range traceOptions.Classifications {
		options = t.AddClassifier(options, c)
	}

	state.m.RLock()
	kfPanic := state.c.PanicOnKillFailure
	rl := state.c.RateLimit
	synchronousPut := state.c.SynchronousPut
	state.m.RUnlock()

	select {
	case <-timeout:
		err = errors.New("Tracer lock acquisition timed out")
		t.Logf(LogError, "%v\n", err)
		return
	case <-traceLock:
		break
	}

	// We now hold the trace lock.
	// Allow another tracer to execute (i.e. by re-populating the
	// traceLock channel) as long as the current tracer has
	// exited.
	defer func() {
		go traceUnlockRL(t, rl)
	}()

	done := make(chan tracerResult, 1)
	tracer := t.Finalize(options)

	go func() {
		t.Logf(LogDebug, "Starting tracer %v\n", tracer)

		var res tracerResult

		res.stdOut, res.err = tracer.Output()
		done <- res

		t.Logf(LogDebug, "Tracer finished execution\n")
	}()

	t.Logf(LogDebug, "Waiting for tracer completion...\n")

	var res tracerResult

	select {
	case <-timeout:
		if err = tracer.Process.Kill(); err != nil {
			t.Logf(LogError,
				"Failed to kill tracer upon timeout: %v\n",
				err)

			if kfPanic {
				t.Logf(LogWarning,
					"PanicOnKillFailure set; "+
						"panicking\n")
				panic(err)
			}
		}

		err = errors.New("Tracer execution timed out")
		t.Logf(LogError, "%v; process killed\n", err)

		return
	case res = <-done:
		break
	}

	// Tracer execution has completed by this point.
	if res.err != nil {
		t.Logf(LogError, "Tracer failed to run: %v\n",
			res.err)
		err = res.err
		return
	}

	if t.PutEnabled() == false {
		return
	}

	putFn := func() error {
		t.Logf(LogDebug, "Uploading snapshot...")

		if err := t.Put(res.stdOut); err != nil {
			t.Logf(LogError, "Failed to upload snapshot: %s",
				err)

			return err
		}

		t.Logf(LogDebug, "Successfully uploaded snapshot\n")

		return nil
	}

	if synchronousPut {
		err = putFn()
	} else {
		t.Logf(LogDebug, "Starting asynchronous put...\n")
		go putFn()
	}

	t.Logf(LogDebug, "Trace request complete\n")

	return
}

// Create a unique error type to use during panic recovery.
type panicError struct {
	v interface{}
}

func (p *panicError) Error() string {
	return fmt.Sprintf("%v", p.v)
}

// Establishes a panic handler that will execute the specified Tracer in
// response. If repanic is true, this will repanic again after Tracer execution
// completes (with the original value returned by recover()).
// This must be used with Go's defer, panic, and recover pattern; see
// https://blog.golang.org/defer-panic-and-recover.
func Recover(t Tracer, repanic bool, options *TraceOptions) {
	if r := recover(); r != nil {
		err, ok := r.(error)
		if !ok {
			// We use the runtime type of the error object for
			// classification (and thus potential grouping);
			// *bcd.PanicError is a more descriptive classifier
			// than something like *errors.errorString.
			err = &panicError{r}
		}

		Trace(t, err, options)

		if repanic {
			panic(r)
		}
	}
}
