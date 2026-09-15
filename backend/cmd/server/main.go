package main

import (
	"log"
	"os"

	"github.com/bytecode/modbus-mapping-gateway/internal/adapter/in/httpapi"
	"github.com/bytecode/modbus-mapping-gateway/internal/adapter/out/modbus"
	"github.com/bytecode/modbus-mapping-gateway/internal/adapter/out/yamlstore"
	"github.com/bytecode/modbus-mapping-gateway/internal/usecase"
)

func main() {
	addr := env("HTTP_ADDR", ":8080")
	mappingFile := env("MAPPING_FILE", "configs/mapping.yaml")
	defaultFile := env("MAPPING_DEFAULT", "configs/mapping.yaml")

	if err := yamlstore.EnsureDefault(mappingFile, defaultFile); err != nil {
		// if same path, ignore; otherwise try continue when file already exists
		if mappingFile != defaultFile {
			log.Printf("ensure default mapping: %v", err)
		}
	}

	store := yamlstore.New(mappingFile)
	client := modbus.NewClient()
	svc, err := usecase.NewGatewayService(store, client)
	if err != nil {
		log.Fatalf("load mapping: %v", err)
	}

	srv := httpapi.NewServer(svc)
	log.Printf("modbus mapping gateway listening on %s, mapping=%s", addr, mappingFile)
	if err := srv.Router().Run(addr); err != nil {
		log.Fatal(err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
