package main

import (
	"os"

	"github.com/codingchipmunk/cameleventbeat/cmd"

	_ "github.com/codingchipmunk/cameleventbeat/include"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
