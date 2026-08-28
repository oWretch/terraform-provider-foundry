package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/oWretch/terraform-provider-foundry/internal/provider"
)

var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with debugger support")
	flag.Parse()

	err := providerserver.Serve(
		context.Background(),
		provider.New(version),
		providerserver.ServeOpts{
			Address:         "registry.terraform.io/oWretch/foundry",
			Debug:           debug,
			ProtocolVersion: 6,
		},
	)
	if err != nil {
		log.Fatal(err)
	}
}
