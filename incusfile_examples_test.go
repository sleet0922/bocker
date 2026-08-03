package main

import (
	"path/filepath"
	"testing"
)

func TestExampleIncusfilesParse(t *testing.T) {
	for _, example := range []struct {
		name  string
		port  int
		image string
	}{
		{name: "go-api", port: 8080, image: "alpine/3.24"},
		{name: "python-api", port: 8000, image: "alpine/3.24"},
		{name: "node-api", port: 3000, image: "alpine/3.24"},
	} {
		t.Run(example.name, func(t *testing.T) {
			f, err := parseIncusfile(filepath.Join("examples", example.name, "Incusfile"))
			if err != nil {
				t.Fatalf("parse Incusfile: %v", err)
			}
			if f.From != example.image || f.Network != "nat" || f.Name == "" {
				t.Fatalf("unexpected metadata: from=%q network=%q name=%q", f.From, f.Network, f.Name)
			}
			if len(f.Exposes) != 1 || f.Exposes[0].Port != example.port || f.Exposes[0].Protocol != "tcp" {
				t.Fatalf("unexpected expose: %#v", f.Exposes)
			}
			if f.Domain == "" || f.Autostart == nil || !*f.Autostart {
				t.Fatalf("runtime metadata missing: domain=%q autostart=%v", f.Domain, f.Autostart)
			}
		})
	}
}
