# bcd
--
    import "."

Package bcd provides integration with out of process tracers. Using the provided
Tracer interface, applications may invoke tracer execution on demand. Panic and
signal handling integrations are provided.

The Tracer interface is generic and will support any out of process tracer
implementing it. A default Tracer implementation, which uses the Backtrace I/O
platform, is provided.

## Usage

```go
const (
	LogDebug = 1 << iota
	LogWarning
	LogError
	LogMax = (1 << iota) - 1
)
```

#### func  EnableTracing

```go
func EnableTracing() error
```
Call this function to allow other (non-parent) processes to trace this one.
Alternatively, set kernel.yama.ptrace_scope = 0 in /etc/sysctl.d/10-ptrace.conf.

#### func  Recover

```go
func Recover(t Tracer, repanic bool, options *TraceOptions)
```
Establishes a panic handler that will execute the specified Tracer in response.
If repanic is true, this will repanic again after Tracer execution completes
(with the original value returned by recover()). This must be used with Go's
defer, panic, and recover pattern; see
https://blog.golang.org/defer-panic-and-recover.

#### func  Register

```go
func Register(t TracerSig)
```
Registers a signal handler to execute the specified Tracer upon receipt of any
signal in the set specified by TracerSig.Sigset(). If the GlobalConfiguration
value ResendSignal is true, then when a signal is received through this handler,
all handlers for that signal will be reset with signal.Reset(s) after tracer
execution completes. The signal will then be resent to the default Go handler
for that signal.

#### func  Trace

```go
func Trace(t Tracer, e error, traceOptions *TraceOptions) (err error)
```
Executes the specified Tracer on the current process.

If e is non-nil, it will be used to augment the trace according to the
TraceOptions. If traceOptions is non-nil, it will be used instead of the
Tracer's DefaultTraceOptions(). See TraceOptions for details on the various
options.

This is goroutine-safe; multiple goroutines may share the same Tracer and
execute Trace() concurrently. Only one tracer will be allowed to run at any
point; others will wait to acquire resources (locks) or timeout (if timeouts are
not disabled). Trace execution will be rate-limited according to the
GlobalConfig settings.

This may also be called in a new goroutine via go Trace(...). In that case,
ensure TraceOptions.CallerOnly is false (you will likely also want to set
TraceOptions.Faulted to false); otherwise, only the newly spawned goroutine will
be traced.

Output of specific Tracer execution depends on the implementation; most Tracers
will have options for specifying output paths.

#### func  Unregister

```go
func Unregister(t TracerSig)
```
Stops the specified TracerSig from handling any signals it was previously
registered to handle via bcd.Register().

#### func  UpdateConfig

```go
func UpdateConfig(c *GlobalConfig)
```
Update global Tracer configuration.

#### type BTTracer

```go
type BTTracer struct {
}
```


#### func  New

```go
func New(includeSystemGs bool) *BTTracer
```
Returns a new object implementing the bcd.Tracer and bcd.TracerSig interfaces.

Relevant default values:

Path: /opt/backtrace/bin/ptrace

Signal set: ABRT, FPE, SEGV, ILL, BUS, INT. Note: Go converts BUS, FPE, and SEGV
arising from process execution into run-time panics, which cannot be handled by
signal handlers. These signals are caught went sent from os.Process.Kill or
similar.

System goroutines (i.e. those started and used by the Go runtime) are excluded
unless the includeSystemGs parameter is true.

The default logger prints to stderr.

DefaultTraceOptions: Faulted: true CallerOnly: false ErrClassification: true
Timeout: 120s

#### func (*BTTracer) AddClassifier

```go
func (t *BTTracer) AddClassifier(options []string, classifier string) []string
```
See bcd.Tracer.AddClassifier().

#### func (*BTTracer) AddFaultedThread

```go
func (t *BTTracer) AddFaultedThread(options []string, tid int) []string
```
See bcd.Tracer.AddFaultedThread().

#### func (*BTTracer) AddKV

```go
func (t *BTTracer) AddKV(options []string, key, val string) []string
```
See bcd.Tracer.AddKV().

#### func (*BTTracer) AddOptions

```go
func (t *BTTracer) AddOptions(options []string, v ...string) []string
```
See bcd.Tracer.AddOptions().

#### func (*BTTracer) AddThreadFilter

```go
func (t *BTTracer) AddThreadFilter(options []string, tid int) []string
```
See bcd.Tracer.AddThreadFilter().

#### func (*BTTracer) ClearOptions

```go
func (t *BTTracer) ClearOptions()
```
See bcd.Tracer.ClearOptions().

#### func (*BTTracer) DefaultTraceOptions

```go
func (t *BTTracer) DefaultTraceOptions() *TraceOptions
```
See bcd.Tracer.DefaultTraceOptions().

#### func (*BTTracer) Finalize

```go
func (t *BTTracer) Finalize(options []string) *exec.Cmd
```
See bcd.Tracer.Finalize().

#### func (*BTTracer) Logf

```go
func (t *BTTracer) Logf(level LogPriority, format string, v ...interface{})
```

#### func (*BTTracer) Options

```go
func (t *BTTracer) Options() []string
```
See bcd.Tracer.Options().

#### func (*BTTracer) SetLogLevel

```go
func (t *BTTracer) SetLogLevel(level LogPriority)
```

#### func (*BTTracer) SetLogger

```go
func (t *BTTracer) SetLogger(logger Log)
```
Sets the logger for the tracer.

#### func (*BTTracer) SetPath

```go
func (t *BTTracer) SetPath(path string)
```
Sets the executable path for the tracer.

#### func (*BTTracer) SetPipes

```go
func (t *BTTracer) SetPipes(stdin io.Reader, stdout, stderr io.Writer)
```
Sets the input and output pipes for the tracer.

#### func (*BTTracer) SetSigchan

```go
func (t *BTTracer) SetSigchan(sc chan os.Signal)
```
See bcd.TracerSig.SetSigchan().

#### func (*BTTracer) SetSigset

```go
func (t *BTTracer) SetSigset(sigs ...os.Signal)
```
See bcd.TracerSig.SetSigset().

#### func (*BTTracer) Sigchan

```go
func (t *BTTracer) Sigchan() chan os.Signal
```
See bcd.TracerSig.Sigchan().

#### func (*BTTracer) Sigset

```go
func (t *BTTracer) Sigset() []os.Signal
```
See bcd.TracerSig.Sigset().

#### func (*BTTracer) String

```go
func (t *BTTracer) String() string
```

#### type GlobalConfig

```go
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
	// Defaults to false.
	ResendSignal bool

	// Length of time to wait after completion of a tracer's
	// execution before allowing the next tracer to run.
	// Defaults to 3 seconds.
	RateLimit time.Duration
}
```


#### type Log

```go
type Log interface {
	// Logs the specified message if the specified log level is enabled.
	Logf(level LogPriority, format string, v ...interface{})

	// Sets the log level to the specified bitmask of LogPriorities; all
	// priorities excluded from the mask are ignored.
	SetLogLevel(level LogPriority)
}
```


#### type LogPriority

```go
type LogPriority int
```


#### type TraceOptions

```go
type TraceOptions struct {
	// If true, the calling thread/goroutine will be marked as faulted
	// (i.e. the cause of the error or trace request).
	Faulted bool

	// If true, only the calling thread/goroutine will be traced; all others
	// will be excluded from the generated snapshot.
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
```

Options determining actions taken during Tracer execution.

#### type Tracer

```go
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
}
```

A generic out of process tracer interface. The methods in this interface are
expected to be goroutine safe; multiple trace requests (which ultimately call
into these methods) from different goroutines may run concurrently.

#### type TracerSig

```go
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
```

This is a superset of the generic Tracer interface for those that wish to
support signal handling. The methods unique to this interface are not expected
to be goroutine-safe.
