package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
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
	name string
	cmd  *exec.Cmd
	in   io.Writer
	out  *bufio.Reader
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

	return &b, nil
}

func (b *benchmark) list() ([]string, error) {
	if _, err := io.WriteString(b.in, "list\n"); err != nil {
		return nil, err
	}
	var bb []string
	for {
		line, err := b.out.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if line == "\n" {
			break
		}
		bb = append(bb, line[:len(line)-1])
	}
	return bb, nil
}

func (b *benchmark) run(benchmark string, n int) (*parse.Benchmark, error) {
	if *debug {
		fmt.Printf("run %s %d\n", benchmark, n)
	}
	if _, err := fmt.Fprintf(b.in, "run %s %d\n", benchmark, n); err != nil {
		return nil, err
	}
	line, err := b.out.ReadString('\n')
	if err != nil {
		return nil, err
	}
	return parse.ParseLine(line)
}

func (b *benchmark) mustRun(benchmark string, n int) *parse.Benchmark {
	p, err := b.run(benchmark, n)
	if err != nil {
		log.Fatal(err)
	}
	return p
}

func main() {
	flag.Parse()
	if flag.NArg() < 1 {
		// TODO
		fmt.Println("rod <compiled tests>")
		os.Exit(2)
	}

	// Start all the tests.
	// Find the intersection of benchmarks contained in all tests
	var tests []*benchmark
	allb := make(map[string]bool)
	for i := 0; i < flag.NArg(); i++ {
		// TODO: Run tests once, if requested, supporting regexp and short
		t, err := newBenchmark(flag.Arg(i))
		if err != nil {
			log.Fatal(err)
		}
		t.name = flag.Arg(i)
		tests = append(tests, t)
		bb, err := t.list()
		if err != nil {
			log.Fatal(err)
		}
		if i == 0 {
			for _, b := range bb {
				allb[b] = true
			}
			continue
		}
		for e := range allb {
			var found bool
			for _, b := range bb {
				if e == b {
					found = true
					break
				}
			}
			if !found {
				delete(allb, e)
			}
		}
	}

	re, err := regexp.Compile(*filter)
	if err != nil {
		log.Fatal(err)
	}
	var matched []string
	for b := range allb {
		if re.MatchString(b) {
			matched = append(matched, b)
		}
	}

	for _, b := range matched {
		for _, t := range tests {
			var sts [2]stats.Stats
			h := 2
			for x := 1; x <= 2; x++ {
				st := &sts[x-1]
				fmt.Println(t.name, b, h)
				for j := 0; j < 100; j++ {
					var p *parse.Benchmark
					if x == 1 {
						p = t.mustRun(b, x)
					} else {
						p = t.mustRun(b, h)
					}
					st.Update(p.NsPerOp * float64(p.N))
					// fmt.Println(x, p.NsPerOp)
				}
				fmt.Printf("n=%d\tcount=%d\tmean=%f\tvar=%f\tskew=%f\tkurt=%f\n",
					x, st.Count(),
					st.Mean(), st.SampleVariance(),
					st.SampleSkew(), st.SampleKurtosis(),
				)
			}
			// fmt.Printf("delta:\tmean=%f\tvar=%f\n",
			// 	sts[1].Mean()-sts[0].Mean(), sts[1].SampleVariance()-sts[0].SampleVariance(),
			// )
			codemean := (sts[1].Mean() - sts[0].Mean()) / float64(h-1)
			overheadmean := sts[0].Mean() - codemean
			// codevar := sts[1].SampleVariance() - sts[0].SampleVariance()
			// overheadvar := sts[0].SampleVariance() - codevar
			// fmt.Printf("code mean=%f var=%f overhead mean=%f var=%f\n", codemean, codevar, overheadmean, overheadvar)
			fmt.Printf("code mean=%f overhead mean=%f\n", codemean, overheadmean)

			// Goal is that the mean should be dominated by the code time, not the overhead.
			// Therefore we want to pick N such that overheadmean = 1% * N * codemean,
			// where 1% is arbitrary. So N = overheadmean / (codemean * target).
			target := 0.01
			n := overheadmean / (codemean * target)
			ndur := time.Duration(n * codemean)
			fmt.Println("TARGET N", int(n), "DUR", ndur)

			// p0 := t.mustRun(b, 3*time.Second)
			// var st stats.Stats
			// var reg stats.Regression
			// for j := 0; j < 200; j++ {
			// 	n := rand.Intn(p0.N) + 1
			// 	p := t.mustRun(b, n)
			// 	// reg.Update(float64(p.N), p.NsPerOp)
			// 	// st.Update(p.NsPerOp)
			// 	fmt.Printf("%f\t%f\n", float64(p.N), p.NsPerOp*float64(p.N))
			// 	// fmt.Printf("%v %v mean=%f skew=%f kurtosis=%f\n",
			// 	// 	t.name, b,
			// 	// 	st.Mean(),
			// 	// 	st.SampleSkew(), st.SampleKurtosis())
			// }
			// _ = st
			// _ = reg
			// fmt.Printf("%v count=%v r2=%.2f slope=%.2f slopeerr=%.2f intercept=%.2f intercepterr=%.2f\n",
			// 	b,
			// 	reg.Count(), reg.RSquared(),
			// 	reg.Slope(), reg.SlopeStandardError(),
			// 	reg.Intercept(), reg.InterceptStandardError(),
			// )

			// 	continue
			// 	n := 1
			// 	avg := func() float64 {
			// 		const N = 3
			// 		var sum float64
			// 		for j := 0; j < N; j++ {
			// 			p := before.mustRun(b, n)
			// 			sum += p.NsPerOp
			// 		}
			// 		sum /= N
			// 		return sum
			// 	}
			// 	prev := avg()
			// 	for {
			// 		n <<= 1
			// 		now := avg()
			// 		if now >= prev {
			// 			break
			// 		}
			// 		prev = now
			// 	}
			// 	fmt.Printf("%v\t%d: %v\n", b, n, int64(prev))

			// 	continue
			// 	// In decreasing time order
			// 	durations := []time.Duration{
			// 		100 * time.Millisecond,
			// 		1 * time.Millisecond,
			// 		10 * time.Microsecond,
			// 	}
			// 	nn := make([]int, len(durations))
			// 	for i := range nn {
			// 		nn[i] = 1
			// 	}
			// 	for i, d := range durations {
			// 		nn[i] = before.itersFor(b, d)
			// 		// We only got one iteration in during this
			// 		// long duration, so we'll only get one is
			// 		// during the shorter ones as well.
			// 		if nn[i] == 1 {
			// 			break
			// 		}
			// 	}
			// 	const iters = 50
			// 	for i := range nn {
			// 		var st stats.Stats
			// 		for iter := 0; iter < iters; iter++ {
			// 			p, err := before.run(b, nn[i])
			// 			if err != nil {
			// 				log.Fatal(err)
			// 			}
			// 			st.Update(p.NsPerOp)
			// 		}
			// 		fmt.Printf("%s\t(%v => %d)\tn=%d\tmean=%f\tvar=%f\n",
			// 			b, durations[i], nn[i], iters, st.Mean(), st.SampleVariance())
			// 	}
			// }
		}
	}
}
