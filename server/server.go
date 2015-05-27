package server

import (
	"errors"
	"flag"
	"log"
	"net"
	"net/rpc"
	"reflect"
	"regexp"
	"runtime"
	"sync"
	"testing"
	"time"
)

var (
	benchserve     = flag.Bool("test.benchserve", false, "run benchmarking server")
	benchserveaddr = flag.String("test.benchserveaddr", ":9998", "benchmarking server address")

	server = new(Server)
)

// Start starts the benchmark server, if the test.benchserve flag is set.
// Start blocks until the server encounters an error if serving.
// If not serving, Start returns immediately.
// We use this to hijack the test running system to allow us to run our benchmark
// server without having to alter main or collide with a user's existing testing.M.
// Set -test.timeout=10000h to allow the server to run as long as needed.
func Start() {
	if *benchserve {
		server.serve()
	}
}

// Register registers the benchmarks bb with the server.
// All calls to register must happen before calls to Start.
// Register is usually called from auto-generated init functions.
func Register(bb []Benchmark) {
	server.benchmarks = append(server.benchmarks, bb...)
}

type Server struct {
	benchmarks []Benchmark
}

// Benchmarks returns the indices of all benchmarks matching filter.
// Benchmarks are not returned by name, because it is possible to have
// duplicate benchmark names, for example when the same benchmark name
// is defined in a test and an external test.
func (s *Server) Benchmarks(filter string, reply *[]int) error {
	re, err := regexp.Compile(filter)
	if err != nil {
		return err
	}

	var ii []int
	for i, b := range s.benchmarks {
		if re.MatchString(b.Name) {
			ii = append(ii, i)
		}
	}
	*reply = ii
	return nil
}

// Run specifies a single benchmark run.
type Run struct {
	I int // the index of the benchmark, as returned by Benchmarks
	N int // the number of iterations to run for
}

// Run executes a single benchmark run.
// TODO: Reply is...what?
func (s *Server) Run(run Run, reply *testing.BenchmarkResult) error {
	if run.I < 0 || run.I >= len(s.benchmarks) {
		return errors.New("bad index") // TODO: better error
	}
	b := s.benchmarks[run.I]
	*reply = b.run(run.N)
	return nil
}

func (s *Server) serve() {
	rpc.Register(s)
	l, err := net.Listen("tcp", *benchserveaddr)
	if err != nil {
		log.Printf("benchserve failed to listen: %v", err)
		return
	}
	rpc.Accept(l)
}

type Benchmark struct {
	Name string
	F    func(*testing.B)
}

// run runs b for the specified number of iterations.
func (b *Benchmark) run(n int) testing.BenchmarkResult {
	var wg sync.WaitGroup
	wg.Add(1)
	tb := testing.B{N: n}
	tb.SetParallelism(1)

	go func() {
		defer wg.Done()

		// Try to get a comparable environment for each run
		// by clearing garbage from previous runs.
		runtime.GC()
		tb.ResetTimer()
		tb.StartTimer()
		b.F(&tb)
		tb.StopTimer()
	}()
	wg.Wait()

	v := reflect.ValueOf(tb)
	r := testing.BenchmarkResult{
		N:         n,
		T:         time.Duration(extractInt(v, "duration")),
		Bytes:     extractInt(v, "bytes"),
		MemAllocs: extractUint(v, "netAllocs"),
		MemBytes:  extractUint(v, "netBytes"),
	}

	return r
}

func extractInt(v reflect.Value, field string) int64 {
	x, ok := v.Type().FieldByName(field)
	if !ok {
		panic("failed to find testing.B field: " + field)
	}
	return v.FieldByIndex(x.Index).Int()
}

func extractUint(v reflect.Value, field string) uint64 {
	x, ok := v.Type().FieldByName(field)
	if !ok {
		panic("failed to find testing.B field: " + field)
	}
	return v.FieldByIndex(x.Index).Uint()
}
