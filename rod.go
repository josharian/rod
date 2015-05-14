package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/GaryBoone/GoStats/stats"
	"golang.org/x/tools/benchmark/parse"
)

var (
	debug  = flag.Bool("debug", false, "add debug output")
	filter = flag.String("bench", ".", "regexp to filter benchmarks")
)

type benchmark struct {
	cmd *exec.Cmd
	in  io.Writer
	out *bufio.Reader
}

func newBenchmark(path string, args ...string) (*benchmark, error) {
	var err error
	var b benchmark
	args = append([]string{"-test.run=NONE", "-test.benchserve"}, args...)
	if *debug {
		fmt.Println("RUN", path, strings.Join(args, " "))
	}
	b.cmd = exec.Command(path, args...)
	if b.in, err = b.cmd.StdinPipe(); err != nil {
		return nil, err
	}
	var stdout io.Reader
	if stdout, err = b.cmd.StdoutPipe(); err != nil {
		return nil, err
	}
	if *debug {
		stdout = io.TeeReader(stdout, os.Stdout)
	}
	b.out = bufio.NewReader(stdout)
	var stderr io.ReadCloser
	stderr, err = b.cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	go func() {
		r := bufio.NewReader(stderr)
		s, err := r.ReadString('\n')
		if err != nil {
			log.Fatal(err)
		} else {
			log.Fatal(s)
		}
	}()
	if err := b.cmd.Start(); err != nil {
		return nil, err
	}

	// Consume any test output
	var s string
	for s != "PASS\n" {
		s, err = b.out.ReadString('\n')
		if err != nil {
			return nil, err
		}
	}

	return &b, nil
}

func (b *benchmark) list() ([]string, error) {
	if _, err := io.WriteString(b.in, "list\n"); err != nil {
		return nil, err
	}
	// Ugly hack, but good enough
	j, err := b.out.ReadBytes(']')
	if err != nil {
		return nil, err
	}
	// Consume the trailing \n
	if _, err := b.out.ReadString('\n'); err != nil {
		return nil, err
	}
	var bb []string
	if err := json.Unmarshal(j, &bb); err != nil {
		return nil, err
	}
	return bb, nil
}

func (b *benchmark) run(benchmark string, length interface{}) (*parse.Benchmark, error) {
	if *debug {
		fmt.Printf("run %s %v\n", benchmark, length)
	}
	if _, err := fmt.Fprintf(b.in, "run %s %v\n", benchmark, length); err != nil {
		return nil, err
	}
	line, err := b.out.ReadString('\n')
	if err != nil {
		return nil, err
	}
	return parse.ParseLine(line)
}

func (b *benchmark) mustRun(benchmark string, length interface{}) *parse.Benchmark {
	p, err := b.run(benchmark, length)
	if err != nil {
		log.Fatal(err)
	}
	return p
}

func (b *benchmark) itersFor(benchmark string, d time.Duration) int {
	p, err := b.run(benchmark, d)
	if err != nil {
		log.Fatal(err)
	}
	return p.N
}

func main() {
	flag.Parse()
	if flag.NArg() < 1 {
		// TODO
		fmt.Println("rod <compiled tests>")
		os.Exit(2)
	}

	// TODO: Run tests once, if requested, supporting regexp and short
	before, err := newBenchmark(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}

	bb, err := before.list()
	if err != nil {
		log.Fatal(err)
	}

	re, err := regexp.Compile(*filter)
	if err != nil {
		log.Fatal(err)
	}
	var matched []string
	for _, b := range bb {
		if re.MatchString(b) {
			matched = append(matched, b)
		}
	}

	for _, b := range matched {
		fmt.Println(b)
		p0 := before.mustRun(b, 2*time.Second)

		var st stats.Stats
		var reg stats.Regression
		for j := 0; j < 100; j++ {
			n := (p0.N / 1000) + rand.Intn(p0.N) + 1
			p := before.mustRun(b, n)
			reg.Update(float64(p.N), p.NsPerOp)
			st.Update(p.NsPerOp)
			// fmt.Printf("%f\t%f\n", float64(p.N), p.NsPerOp)
			fmt.Printf("mean=%f skew=%f kurtosis=%f\n", st.Mean(), st.SampleSkew(), st.SampleKurtosis())
		}
		fmt.Printf("%v count=%v r2=%.2f slope=%.2f slopeerr=%.2f intercept=%.2f intercepterr=%.2f\n",
			b,
			reg.Count(), reg.RSquared(),
			reg.Slope(), reg.SlopeStandardError(),
			reg.Intercept(), reg.InterceptStandardError(),
		)

		continue
		n := 1
		avg := func() float64 {
			const N = 3
			var sum float64
			for j := 0; j < N; j++ {
				p := before.mustRun(b, n)
				sum += p.NsPerOp
			}
			sum /= N
			return sum
		}
		prev := avg()
		for {
			n <<= 1
			now := avg()
			if now >= prev {
				break
			}
			prev = now
		}
		fmt.Printf("%v\t%d: %v\n", b, n, int64(prev))

		continue
		// In decreasing time order
		durations := []time.Duration{
			100 * time.Millisecond,
			1 * time.Millisecond,
			10 * time.Microsecond,
		}
		nn := make([]int, len(durations))
		for i := range nn {
			nn[i] = 1
		}
		for i, d := range durations {
			nn[i] = before.itersFor(b, d)
			// We only got one iteration in during this
			// long duration, so we'll only get one is
			// during the shorter ones as well.
			if nn[i] == 1 {
				break
			}
		}
		const iters = 50
		for i := range nn {
			var st stats.Stats
			for iter := 0; iter < iters; iter++ {
				p, err := before.run(b, nn[i])
				if err != nil {
					log.Fatal(err)
				}
				st.Update(p.NsPerOp)
			}
			fmt.Printf("%s\t(%v => %d)\tn=%d\tmean=%f\tvar=%f\n",
				b, durations[i], nn[i], iters, st.Mean(), st.SampleVariance())
		}
	}
}
