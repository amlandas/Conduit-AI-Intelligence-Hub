package runtime

import (
	"context"
	"testing"
)

func TestNewSelector(t *testing.T) {
	s := NewSelector("podman")
	if s == nil {
		t.Error("NewSelector returned nil")
	}
	if s.preferred != "podman" {
		t.Errorf("preferred mismatch: got %s, want podman", s.preferred)
	}
}

func TestNewSelector_Docker(t *testing.T) {
	s := NewSelector("docker")
	if s.preferred != "docker" {
		t.Errorf("preferred mismatch: got %s, want docker", s.preferred)
	}
}

func TestNewSelector_Auto(t *testing.T) {
	s := NewSelector("")
	if s.preferred != "" {
		t.Errorf("preferred should be empty for auto, got %s", s.preferred)
	}
}

func TestDetectAll(t *testing.T) {
	s := NewSelector("")
	ctx := context.Background()

	runtimes := s.DetectAll(ctx)

	// Should return info for both podman and docker
	if len(runtimes) != 2 {
		t.Errorf("expected 2 runtimes, got %d", len(runtimes))
	}

	// Check that we have podman and docker entries
	names := make(map[string]bool)
	for _, r := range runtimes {
		names[r.Name] = true
	}

	if !names["podman"] {
		t.Error("missing podman in DetectAll")
	}
	if !names["docker"] {
		t.Error("missing docker in DetectAll")
	}
}

func TestNewPodmanProvider(t *testing.T) {
	p := NewPodmanProvider()
	if p == nil {
		t.Error("NewPodmanProvider returned nil")
	}
	if p.Name() != "podman" {
		t.Errorf("Name mismatch: got %s, want podman", p.Name())
	}
}

func TestNewDockerProvider(t *testing.T) {
	p := NewDockerProvider()
	if p == nil {
		t.Error("NewDockerProvider returned nil")
	}
	if p.Name() != "docker" {
		t.Errorf("Name mismatch: got %s, want docker", p.Name())
	}
}

func TestParseKeyValue(t *testing.T) {
	tests := []struct {
		input    string
		wantKey  string
		wantVal  string
	}{
		{"key=value", "key", "value"},
		{"key=value=with=equals", "key", "value=with=equals"},
		{"keyonly", "keyonly", ""},
		{"empty=", "empty", ""},
	}

	for _, tt := range tests {
		k, v := parseKeyValue(tt.input)
		if k != tt.wantKey || v != tt.wantVal {
			t.Errorf("parseKeyValue(%q) = (%q, %q), want (%q, %q)",
				tt.input, k, v, tt.wantKey, tt.wantVal)
		}
	}
}

func TestFormatEnv(t *testing.T) {
	env := map[string]string{
		"KEY1": "value1",
		"KEY2": "value2",
	}

	result := formatEnv(env)
	if len(result) != 2 {
		t.Errorf("expected 2 env vars, got %d", len(result))
	}

	// Check that both are present (order may vary due to map iteration)
	found := make(map[string]bool)
	for _, e := range result {
		found[e] = true
	}

	if !found["KEY1=value1"] {
		t.Error("missing KEY1=value1")
	}
	if !found["KEY2=value2"] {
		t.Error("missing KEY2=value2")
	}
}

func TestContainerSpec_Defaults(t *testing.T) {
	spec := ContainerSpec{
		Name:  "test",
		Image: "alpine:latest",
	}

	if spec.Name != "test" {
		t.Errorf("Name mismatch: got %s, want test", spec.Name)
	}
	if spec.Image != "alpine:latest" {
		t.Errorf("Image mismatch: got %s, want alpine:latest", spec.Image)
	}
	if spec.Stdin {
		t.Error("Stdin should default to false")
	}
}

func TestSecuritySpec_Defaults(t *testing.T) {
	spec := SecuritySpec{
		ReadOnlyRootfs:   true,
		NoNewPrivileges:  true,
		DropCapabilities: []string{"ALL"},
	}

	if !spec.ReadOnlyRootfs {
		t.Error("ReadOnlyRootfs should be true")
	}
	if !spec.NoNewPrivileges {
		t.Error("NoNewPrivileges should be true")
	}
	if len(spec.DropCapabilities) != 1 || spec.DropCapabilities[0] != "ALL" {
		t.Error("DropCapabilities should be [ALL]")
	}
}

func TestNetworkSpec_Modes(t *testing.T) {
	tests := []struct {
		mode     string
		expected string
	}{
		{"none", "none"},
		{"bridge", "bridge"},
		{"host", "host"},
	}

	for _, tt := range tests {
		spec := NetworkSpec{Mode: tt.mode}
		if spec.Mode != tt.expected {
			t.Errorf("Mode mismatch: got %s, want %s", spec.Mode, tt.expected)
		}
	}
}

func TestResourceSpec(t *testing.T) {
	spec := ResourceSpec{
		MemoryMB: 512,
		CPUs:     1.5,
	}

	if spec.MemoryMB != 512 {
		t.Errorf("MemoryMB mismatch: got %d, want 512", spec.MemoryMB)
	}
	if spec.CPUs != 1.5 {
		t.Errorf("CPUs mismatch: got %f, want 1.5", spec.CPUs)
	}
}

func TestMount(t *testing.T) {
	m := Mount{
		Source:   "/host/path",
		Target:   "/container/path",
		ReadOnly: true,
	}

	if m.Source != "/host/path" {
		t.Errorf("Source mismatch: got %s", m.Source)
	}
	if m.Target != "/container/path" {
		t.Errorf("Target mismatch: got %s", m.Target)
	}
	if !m.ReadOnly {
		t.Error("ReadOnly should be true")
	}
}

func TestPort(t *testing.T) {
	p := Port{
		Host:      8080,
		Container: 80,
		Protocol:  "tcp",
	}

	if p.Host != 8080 {
		t.Errorf("Host port mismatch: got %d", p.Host)
	}
	if p.Container != 80 {
		t.Errorf("Container port mismatch: got %d", p.Container)
	}
	if p.Protocol != "tcp" {
		t.Errorf("Protocol mismatch: got %s", p.Protocol)
	}
}

func TestRuntimeInfo(t *testing.T) {
	info := RuntimeInfo{
		Name:      "podman",
		Available: true,
		Version:   "4.5.0",
		Preferred: true,
	}

	if info.Name != "podman" {
		t.Errorf("Name mismatch: got %s", info.Name)
	}
	if !info.Available {
		t.Error("Available should be true")
	}
	if info.Version != "4.5.0" {
		t.Errorf("Version mismatch: got %s", info.Version)
	}
	if !info.Preferred {
		t.Error("Preferred should be true")
	}
}
