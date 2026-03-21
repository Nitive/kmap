package main

import (
	"log"
	"os"

	"keyboard/cmd/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		log.Fatalf("kmap failed: %v", err)
	}
}
