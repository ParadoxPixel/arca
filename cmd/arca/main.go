package main

import (
	"fmt"
	"os"
	"os/signal"
	"slices"
	"syscall"

	"github.com/charmbracelet/log"

	"arca/internal/config"
	"arca/internal/server"
	"arca/internal/version"
)

func main() {
	build := version.Current()

	if slices.Contains(os.Args[1:], "--version") || slices.Contains(os.Args[1:], "-version") {
		fmt.Printf("arca %s (%s) built %s\n", build.Version, build.Short(), build.BuildTime)
		return
	}

	configuration, err := config.Parse(os.Args[1:])
	if err != nil {
		os.Exit(2)
	}

	instance, err := server.New(configuration)
	if err != nil {
		log.Fatalf("startup failed: %v", err)
	}

	log.Infof("arca %s (%s) built %s", build.Version, build.Short(), build.BuildTime)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-signals
		instance.Shutdown()
	}()

	if err := instance.Start(); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
