package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type config struct {
	Service struct {
		Name    string `yaml:"name"`
		Message string `yaml:"message"`
		Version string `yaml:"version"`
	} `yaml:"service"`
	Limits struct {
		RequestTimeoutMS int `yaml:"request_timeout_ms"`
		MaxBodyBytes     int `yaml:"max_body_bytes"`
	} `yaml:"limits"`
}

func loadConfig() config {
	path := os.Getenv("APP_CONFIG")
	if path == "" {
		path = "/etc/go-api/config.yaml"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("read config: %v", err)
	}
	var cfg config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("parse config: %v", err)
	}
	if value := os.Getenv("APP_MESSAGE"); value != "" {
		cfg.Service.Message = value
	}
	if value := os.Getenv("APP_VERSION"); value != "" {
		cfg.Service.Version = value
	}
	if value := os.Getenv("APP_TIMEOUT_MS"); value != "" {
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			cfg.Limits.RequestTimeoutMS = n
		}
	}
	return cfg
}

func main() {
	cfg := loadConfig()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","language":"go"}`))
	})
	mux.HandleFunc("/config", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cfg)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintf(w, "%s (%s)\n", cfg.Service.Message, cfg.Service.Version)
	})
	addr := ":8080"
	if value := os.Getenv("APP_ADDR"); value != "" {
		addr = value
	}
	log.Printf("%s listening on %s", cfg.Service.Name, addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
