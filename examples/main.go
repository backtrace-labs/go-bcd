package main

import (
	"github.com/backtrace-labs/go-bcd"

	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"
)

const (
	max_recurse = 2
)

var (
	tracer *bcd.BTTracer
	wg sync.WaitGroup
)

func pan() {
	defer bcd.Recover(tracer, false, nil)

	panic("panic error")
}

func sig() {
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		fmt.Println("error: failed to find process object")
		return
	}

	p.Signal(syscall.SIGSEGV)
}

func recurse(depth int, s1 fishface) {
	if depth == 0 {
		fmt.Println("Sending signal...")
		sig()
		fmt.Println("Signal recovered successfully")

		fmt.Println("Panicking...")
		pan()
		fmt.Println("Panic recovered successfully")

		return
	}

	a := 10
	b := "foo"
	var h string
	i := ""

	f := make(chan string, 3)
	f <- "this"
	f <- "is"
	f <- "Go"
	b = <-f

	c := []int{3, 4, 5}
	var d [3]int
	g := [3]int{7, 8, 9}
	m := [300]string{"test"}

	k := &sarlmons{a: 3, b: 4, c: 5, d: "fish"}

	j := map[string]int{}
	for z := 0; z < 300; z++ {
		j[strconv.Itoa(z)] = z
	}
	e := map[string]int{"a": 10, "b": 5}
	l := map[sarlmons]string{
		sarlmons{3, 4, 5, "fish"}: "what",
		sarlmons{4, 5, 6, "fush"}: "the",
		sarlmons{5, 6, 7, "fisheded"}: "chicken",
	}

	_, _, _, _, _, _, _, _, _, _, _, _ = a, b, c, d, e, f, g, h, i, k, l, m

	go func() {
		fmt.Println("Requesting trace...")
		wg.Add(1)

		err := errors.New("trace-request")

		// Request a trace. TraceOptions are optional -- see pan()
		// for an example of use with the default options.
		traceErr := bcd.Trace(tracer, err, &bcd.TraceOptions{
			// Note: no (unlimited) timeout.
			// Faulted and CallerOnly options don't make sense
			// for asynchronous trace requests. See below for a
			// synchronous request.
			Faulted: false,
			CallerOnly: false,
			ErrClassification: true,
			Classifications: []string{
				"these", "are", "test", "classifiers"}})
		if traceErr != nil {
			fmt.Println("Failed to trace: %v", traceErr)
		}

		wg.Done()
		fmt.Println("Done")
	}()

	go func() {
		wg.Add(1)

		f, err := os.Create("/tmp/dat1")
		if (err != nil) {
			panic(err)
		}
		defer f.Close()

		x := 0
		y := map[sarlmons]string{sarlmons{4, 5, 6, "what"}: "stuff"}

		for {
			x += 1
			f.WriteString(fmt.Sprintf("%d", x))
			f.WriteString(y[sarlmons{4, 5, 6, "what"}])
			f.Sync()

			if x >= 1000 {
				rf, err := os.OpenFile(
					"/home/djoseph/rdonlyfile",
					os.O_RDWR, 0644)

				if err != nil {
					fmt.Println("Requesting trace")
					bcd.Trace(tracer, err, &bcd.TraceOptions{
						Faulted: true,
						CallerOnly: true,
						Timeout: time.Second * 30,
						ErrClassification: true})
					break
				}

				fmt.Println("Writing to rf")
				rf.WriteString("File opened\n")
				rf.Sync()
			}
		}

		wg.Done()
	}()

	recurse(depth - 1, s1)
}

func start() {
	x := &sarlmons{a: 3, b: 4, c: 5, d: "fish"}
	x.b += 3

	recurse(max_recurse, x)
}

func main() {
	if err := bcd.EnableTracing(); err != nil {
		fmt.Println("Failed to enable tracing:", err)
		panic(err)
	}

	// Use the default tracer implementation.
	// false: Exclude system goroutines.
	tracer = bcd.New(false)

	// Enable WARNING log output from the tracer.
	tracer.AddOptions(nil, "-L", "WARNING")

	tracer.AddKV(nil, "version", "1.2.3")

	// Tracer I/O is directed to os.DevNull by default.
	// Note: this does not effect the generated output file (unless the
	// tracer can only print to stdout/err).
	f, err := os.Create("./tracelog")
	if (err != nil) {
		panic(err)
	}
	defer f.Close()
	tracer.SetPipes(nil, f, f)

	tracer.SetLogLevel(bcd.LogDebug)

	// Register for signal handling using the tracer's default signal set.
	bcd.Register(tracer)

	start()

	wg.Wait()
}

type sarlmons struct {
	a, b, c int
	d string
}

type fish map[sarlmons]string

type moop struct {
	a, b, c int
	d string
	e, f map[string]int
}

type fntest struct {
	a, b int
	f func(a, b int) int
}

func intfn(a, b int) int {
	return a + b
}

type fishface interface {
	Plip(i, y int) (result int, err error)
	Plop(i []int) (result int, err error)
	Dunk(i, y string) (result string, err error)
}

func (p *sarlmons) Plip(i, y int) (result int, err error) {
	return i + y, nil
}

func (p *sarlmons) Plop(i []int) (result int, err error) {
	r := 0

	for _, z := range i {
		r += z
	}

	return r, nil
}

func (p *sarlmons) Dunk(i, y string) (result string, err error) {
	return i + y, nil
}
