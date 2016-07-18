package bcd

import (
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type TestTracer struct {
	tracer *exec.Cmd
	options []string
	sleepDuration int
	m sync.Mutex
}

func (t *TestTracer) AddOptions(options []string, v ...string) []string {
	if options != nil {
		return append(options, v...)
	}

	t.m.Lock()
	defer t.m.Unlock()

	t.options = append(t.options, v...)
	return nil
}

func (t *TestTracer) AddKV(options []string, key, val string) []string {
	return t.AddOptions(options, key+":"+val)
}

func (t *TestTracer) AddThreadFilter(options []string, tid int) []string {
	return t.AddOptions(options, "filter:"+strconv.Itoa(tid))
}

func (t *TestTracer) AddFaultedThread(options []string, tid int) []string {
	return t.AddOptions(options, "fault:"+strconv.Itoa(tid))
}

func (t *TestTracer) AddClassifier(options []string, classifier string) []string {
	return t.AddOptions(options, classifier)
}

func (t *TestTracer) Options() []string {
	return t.options
}

func (t *TestTracer) ClearOptions() {
	t.options = nil
}

func (t *TestTracer) DefaultTraceOptions() *TraceOptions {
	return &TraceOptions{
		Faulted:           true,
		CallerOnly:        false,
		ErrClassification: true,
		Timeout:           time.Second * 3}
}

func (t *TestTracer) Finalize(options []string) *exec.Cmd {
	t.m.Lock()
	defer t.m.Unlock()

	t.tracer = exec.Command("/bin/sleep", strconv.Itoa(t.sleepDuration))
	return t.tracer
}

func (t *TestTracer) Put(snapshot []byte) error {
	return nil
}

func (t *TestTracer) PutEnabled() bool {
	return false
}

func (t *TestTracer) Logf(level LogPriority, format string, v ...interface{}) {
}

func (t *TestTracer) SetLogLevel(level LogPriority) {
}

func (t *TestTracer) String() string {
	return "TestTracer"
}

func TestConcurrentRateLimit(t *testing.T) {
	tracer := &TestTracer{}
	count := new(int64)
	var wg sync.WaitGroup
	var rateLimit time.Duration = 3

	UpdateConfig(&GlobalConfig{
		PanicOnKillFailure: true,
		RateLimit: time.Second * rateLimit})

	ng := 4
	timeout := time.After(time.Second * 9)
	done := make(chan struct{}, ng)
	start := time.Now()

	for i := 0; i < ng; i++ {
		wg.Add(1)

		go func() {
			for {
				select {
				case <-done:
					wg.Done()
					return
				default:
				}

				if Trace(tracer, nil, nil) == nil {
					atomic.AddInt64(count, 1)
				}
			}
		}()
	}

	<-timeout
	close(done)
	wg.Wait()
	end := time.Now()
	expected := end.Sub(start) / rateLimit

	if *count > int64(expected) {
		t.Fatalf("Rate limit exceeded: actual %v, expected %v\n",
			*count, expected)
	}
}

func TestTimeout(t *testing.T) {
	UpdateConfig(&GlobalConfig{RateLimit: 0})

	tracer := &TestTracer{sleepDuration: 5}

	traceErr := Trace(tracer, nil, &TraceOptions{
		Timeout: time.Second * 4})
	if traceErr == nil {
		t.Fatal("Trace timeout failed")
	} else if strings.Contains(traceErr.Error(), "execution timed out") == false {
		t.Fatalf("Tracer failure not due to timeout (%v)\n", traceErr)
	}

	// Use the default tracer timeout (3 seconds -- see above).
	traceErr = Trace(tracer, nil, nil)
	if traceErr == nil {
		t.Fatal("Trace timeout failed")
	} else if strings.Contains(traceErr.Error(), "execution timed out") == false {
		t.Fatal("Tracer failure not due to timeout:", traceErr)
	}

	// We shouldn't timeout with a negative timeout.
	traceErr = Trace(tracer, nil, &TraceOptions{
		Timeout: time.Second * -1})
	if traceErr != nil {
		t.Fatal("Tracer failed:", traceErr)
	}
}

func TestTrace(t *testing.T) {
	var tracer TestTracer

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	err := errors.New("Trace error")
	traceErr := Trace(&tracer, err, &TraceOptions{
		Faulted: true,
		CallerOnly: true,
		Timeout: time.Second * 30,
		ErrClassification: true,
		Classifications: []string{"classifier1", "classifier2"}})

	if traceErr != nil {
		t.Fatal("Failed to trace:", traceErr)
	}

	if !tracer.tracer.ProcessState.Success() {
		t.Fatal("Failed to execute tracer successfully")
	}

	expectedSet := map[string]bool{
		"error:"+err.Error(): false,
		"*errors.errorString": false,
		"classifier1": false,
		"classifier2": false}

	if tid, err := gettid(); err == nil {
		expectedSet["fault:"+strconv.Itoa(tid)] = false
		expectedSet["filter:"+strconv.Itoa(tid)] = false
	}

	opns := tracer.Options()
	for _, s := range opns {
		delete(expectedSet, s)
	}

	if len(expectedSet) != 0 {
		t.Fatal("Expected options not set:", expectedSet)
	}
}

func pan(t *TestTracer) {
	defer Recover(t, false, &TraceOptions{
		CallerOnly: false,
		Timeout: time.Second * 30})

	panic("panic")
}

func TestRecover(t *testing.T) {
	var tracer TestTracer
	pan(&tracer)
	if !tracer.tracer.ProcessState.Success() {
		t.Fatal("Failed to recover from panic and trace")
	}
}
