package main

import (
	"flag"
	"interestBar/cmd/apps"
)

func main() {
	var config string
	flag.StringVar(&config, "c", "configs/config.yaml", "choose config file.")
	flag.Parse()

	apps.Run(config)
}
