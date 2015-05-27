// +build ignore

// This is a sample benchmark runner.

package main

import (
	"fmt"
	"log"
	"net/rpc"
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
}
