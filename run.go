// +build ignore

// This is a sample benchmark runner.

package main

import (
	"fmt"
	"log"
	"net/rpc"
	"testing"
)

func main() {
	cli, err := rpc.Dial("tcp", ":9998")
	if err != nil {
		log.Fatal(err)
	}

	var bb []int
	if err := cli.Call("Server.Benchmarks", ".", &bb); err != nil {
		log.Fatal(err)
	}
	fmt.Println(bb)

	for _, i := range bb {
		var res testing.BenchmarkResult
		run := struct {
			I int // the index of the benchmark, as returned by Benchmarks
			N int // the number of iterations to run for
		}{
			I: i,
			N: 50,
		}
		if err := cli.Call("Server.Run", run, &res); err != nil {
			log.Fatal(err)
		}
		fmt.Println(res)
	}
}
