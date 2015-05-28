package server

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"
)

func Main(m *testing.M) {
	benchserve := flag.Bool("test.benchserve", false, "run an interactive benchmark server")
	flag.Parse()
	if !*benchserve {
		os.Exit(m.Run())
	}

	s := server{benchmarks: extractBenchmarks(m)}
	s.serve()
}

func extractBenchmarks(m *testing.M) []testing.InternalBenchmark {
	v := reflect.ValueOf(m).Elem().FieldByName("benchmarks")
	return *(*[]testing.InternalBenchmark)(unsafe.Pointer(v.UnsafeAddr())) // :(((
}

type server struct {
	benchmarks []testing.InternalBenchmark
	benchmem   bool
}

func (s *server) serve() {
	cmds := map[string]func([]string){
		"help": s.cmdHelp,
		"quit": s.cmdQuit,
		"exit": s.cmdQuit,
		"list": s.cmdList,
		"run":  s.cmdRun,
		"set":  s.cmdSet,
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			s.cmdHelp(nil)
			continue
		}
		cmd := cmds[fields[0]]
		if cmd == nil {
			s.cmdHelp(nil)
			continue
		}
		cmd(fields[1:])
	}
	if err := scanner.Err(); err != nil {
		fmt.Println(err)
		os.Exit(2)
	}
}

func (s *server) cmdHelp([]string) {
	fmt.Fprintln(os.Stderr, "commands: help, list, run, set, quit, exit")
}

func (s *server) cmdQuit([]string) {
	os.Exit(0)
}

func (s *server) cmdList([]string) {
	for _, b := range s.benchmarks {
		fmt.Println(b.Name)
	}
	fmt.Println()
}

func (s *server) cmdSet(args []string) {
	// TODO: What else is worth setting?
	if len(args) < 2 || args[0] != "benchmem" {
		fmt.Fprintln(os.Stderr, "set benchmem <bool>")
		return
	}
	b, err := strconv.ParseBool(args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad benchmem value:", err)
		return
	}
	s.benchmem = b
}

func (s *server) cmdRun(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "run <name>[-cpu] <iterations>")
		return
	}

	name := args[0]
	procs := 1
	if i := strings.IndexByte(name, '-'); i != -1 {
		var err error
		procs, err = strconv.Atoi(name[i+1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "bad cpu value:", err)
			return
		}
		name = name[:i]
	}

	var bench testing.InternalBenchmark
	for _, x := range s.benchmarks {
		if x.Name == name {
			bench = x
			// It is possible to define a benchmark with the same name
			// twice in a single test binary, by defining it once
			// in a regular test package and once in an external test package.
			// If you do that, you probably deserve what happens to you now,
			// namely that we run one of the two, but no guarantees which.
			// If someday we combine multiple packages into a single
			// test binary, then we'll probably need to invoke benchmarks
			// by index rather than by name.
			break
		}
	}
	if bench.Name == "" {
		fmt.Fprintln(os.Stderr, "benchmark not found:", name)
		return
	}

	iters, err := strconv.Atoi(args[1])
	if err != nil || iters <= 0 {
		fmt.Fprintf(os.Stderr, "iterations must be positive, got %v\n", iters)
		return
	}

	benchName := benchmarkName(bench.Name, procs)
	fmt.Print(benchName, "\t")

	runtime.GOMAXPROCS(procs)
	r := runBenchmark(bench, iters)

	if r.Failed {
		fmt.Fprintln(os.Stderr, "--- FAIL:", benchName)
		return
	}
	fmt.Print(r.BenchmarkResult)
	if s.benchmem || r.ShowAllocResult {
		fmt.Print("\t", r.MemString())
	}
	fmt.Println()
	if p := runtime.GOMAXPROCS(-1); p != procs {
		fmt.Fprintf(os.Stderr, "testing: %s left GOMAXPROCS set to %d\n", benchName, p)
	}
}

// benchmarkName returns full name of benchmark including procs suffix.
func benchmarkName(name string, n int) string {
	if n != 1 {
		return fmt.Sprintf("%s-%d", name, n)
	}
	return name
}

type Result struct {
	testing.BenchmarkResult
	Failed          bool
	ShowAllocResult bool
}

// runBenchmark runs b for the specified number of iterations.
func runBenchmark(b testing.InternalBenchmark, n int) Result {
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
	var r Result
	r.N = n
	r.T = time.Duration(v.FieldByName("duration").Int())
	r.Bytes = v.FieldByName("bytes").Int()
	r.MemAllocs = v.FieldByName("netAllocs").Uint()
	r.MemBytes = v.FieldByName("netBytes").Uint()
	r.Failed = v.FieldByName("failed").Bool()
	r.ShowAllocResult = v.FieldByName("showAllocResult").Bool()
	return r
}
