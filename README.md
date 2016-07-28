# bcd
--
    import "github.com/backtrace-labs/go-bcd"

Package bcd provides integration with out of process tracers. Using the provided
Tracer interface, applications may invoke tracer execution on demand. Panic and
signal handling integrations are provided.

The Tracer interface is generic and will support any out of process tracer
implementing it. A default Tracer implementation, which uses the Backtrace I/O
platform, is provided.

## Usage

See the [godoc page](https://godoc.org/github.com/backtrace-labs/go-bcd) for
current documentation;
see [this](https://github.com/backtrace-labs/go-bcd/blob/master/examples/main.go)
for an example application.
