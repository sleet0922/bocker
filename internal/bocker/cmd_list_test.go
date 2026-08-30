package bocker

import "testing"

func TestContainerMemoryUsage(t *testing.T) {
	container := &Container{Status: "Running", State: &ContainerState{MemoryUsage: 2 * 1024 * 1024}}
	got, bytes := containerMemoryUsage(container)
	if got != "2.0M" {
		t.Fatalf("containerMemoryUsage() display = %q", got)
	}
	if bytes == nil || *bytes != 2*1024*1024 {
		t.Fatalf("containerMemoryUsage() bytes = %v", bytes)
	}
}

func TestContainerMemoryUsageUnavailable(t *testing.T) {
	for _, container := range []*Container{
		nil,
		{Status: "stopped", State: &ContainerState{MemoryUsage: 42}},
		{Status: "running"},
		{Status: "running", State: &ContainerState{MemoryUsage: -1}},
	} {
		got, bytes := containerMemoryUsage(container)
		if got != "-" || bytes != nil {
			t.Fatalf("containerMemoryUsage() = (%q, %v), want (-, nil)", got, bytes)
		}
	}
}
