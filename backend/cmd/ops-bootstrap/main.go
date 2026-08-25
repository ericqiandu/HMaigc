package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"infinite-canvas/backend/internal/opsbootstrap"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "import" {
		fmt.Fprintln(os.Stderr, "usage: hmaigc-ops-bootstrap import --source-db PATH --source-env PATH --state-root PATH --controller-image IMAGE@sha256:DIGEST --controller-version VERSION --protocol-version 1 --state-volume NAME")
		if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "help") {
			return
		}
		os.Exit(2)
	}
	flags := flag.NewFlagSet("import", flag.ContinueOnError)
	input := opsbootstrap.Input{}
	flags.StringVar(&input.SourceDatabase, "source-db", "", "legacy controller SQLite database")
	flags.StringVar(&input.SourceEnvironment, "source-env", "", "legacy production environment")
	flags.StringVar(&input.StateRoot, "state-root", "", "mounted ops-state root")
	flags.StringVar(&input.ControllerImage, "controller-image", "", "immutable controller image")
	flags.StringVar(&input.ControllerVersion, "controller-version", "", "controller release version")
	flags.StringVar(&input.ProtocolVersion, "protocol-version", "", "durable protocol version")
	flags.StringVar(&input.StateVolume, "state-volume", "", "Docker ops-state volume name")
	if err := flags.Parse(os.Args[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Fatal(err)
	}
	if flags.NArg() != 0 {
		log.Fatal("unexpected positional bootstrap arguments")
	}
	if err := opsbootstrap.Bootstrap(input); err != nil {
		log.Fatal(err)
	}
	fmt.Println("durable ops-state bootstrap completed")
}
