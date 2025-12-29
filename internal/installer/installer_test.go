package installer

import (
	"context"
	"runtime"
	"testing"
)

func TestNew(t *testing.T) {
	inst := New(false)
	if inst == nil {
		t.Fatal("New returned nil")
	}
	if inst.verbose != false {
		t.Error("expected verbose to be false")
	}

	inst = New(true)
	if inst.verbose != true {
		t.Error("expected verbose to be true")
	}
}

func TestInstaller_commandExists(t *testing.T) {
	inst := New(false)

	// Test with a command that should exist on all systems
	var testCmd string
	switch runtime.GOOS {
	case "windows":
		testCmd = "cmd"
	default:
		testCmd = "sh"
	}

	if !inst.commandExists(testCmd) {
		t.Errorf("expected %s to exist", testCmd)
	}

	// Test with a command that shouldn't exist
	if inst.commandExists("definitely-not-a-real-command-12345") {
		t.Error("expected non-existent command to return false")
	}
}

func TestInstaller_getCommandOutput(t *testing.T) {
	inst := New(false)

	// Test with a simple command
	var output string
	switch runtime.GOOS {
	case "windows":
		output = inst.getCommandOutput("cmd", "/c", "echo", "hello")
	default:
		output = inst.getCommandOutput("echo", "hello")
	}

	if output == "" {
		t.Error("expected non-empty output from echo command")
	}
}

func TestInstaller_detectLinuxDistro(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only test")
	}

	inst := New(false)
	distro := inst.detectLinuxDistro()

	// Should return something (could be "unknown")
	if distro == "" {
		t.Error("expected non-empty distro string")
	}

	// Known valid distros
	validDistros := []string{"ubuntu", "debian", "fedora", "rhel", "centos", "arch", "unknown"}
	found := false
	for _, v := range validDistros {
		if distro == v {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("unexpected distro: %s", distro)
	}
}

func TestInstallResult_Fields(t *testing.T) {
	result := InstallResult{
		Dependency:    "Docker",
		Installed:     true,
		AlreadyExists: false,
		Skipped:       false,
		Error:         nil,
		Message:       "Docker installed successfully",
	}

	if result.Dependency != "Docker" {
		t.Error("expected Dependency to be Docker")
	}
	if !result.Installed {
		t.Error("expected Installed to be true")
	}
	if result.AlreadyExists {
		t.Error("expected AlreadyExists to be false")
	}
	if result.Skipped {
		t.Error("expected Skipped to be false")
	}
	if result.Error != nil {
		t.Error("expected Error to be nil")
	}
	if result.Message != "Docker installed successfully" {
		t.Error("expected Message to be set correctly")
	}
}

func TestDependency_Fields(t *testing.T) {
	dep := Dependency{
		Name:        "Docker",
		Description: "Container runtime",
		CheckCmd:    []string{"docker", "version"},
		Required:    true,
	}

	if dep.Name != "Docker" {
		t.Error("expected Name to be Docker")
	}
	if dep.Description != "Container runtime" {
		t.Error("expected Description to be set correctly")
	}
	if len(dep.CheckCmd) != 2 {
		t.Error("expected CheckCmd to have 2 elements")
	}
	if !dep.Required {
		t.Error("expected Required to be true")
	}
}

func TestInstaller_isOllamaRunning_NotInstalled(t *testing.T) {
	inst := New(false)

	// If curl isn't available, this might fail differently
	// but on most systems, if Ollama isn't running, this should return false
	if !inst.commandExists("curl") {
		t.Skip("curl not available")
	}

	// This test just ensures the function doesn't panic
	// We can't guarantee Ollama's state on any given system
	_ = inst.isOllamaRunning()
}

func TestInstaller_ollamaModelExists_NotInstalled(t *testing.T) {
	inst := New(false)

	// If Ollama isn't installed, this should return false
	if !inst.commandExists("ollama") {
		result := inst.ollamaModelExists("qwen2.5-coder:7b")
		if result {
			t.Error("expected false when Ollama is not installed")
		}
	}
}

func TestInstaller_IsDaemonRunning(t *testing.T) {
	inst := New(false)

	// This test just ensures the function doesn't panic
	// We can't guarantee daemon state on any given system
	_ = inst.IsDaemonRunning()
}

func TestInstaller_StopDaemonService_Unsupported(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Only testing unsupported OS behavior")
	}

	inst := New(false)
	err := inst.StopDaemonService()
	if err == nil {
		t.Error("expected error on unsupported OS")
	}
}

func TestInstaller_RemoveDaemonService_Unsupported(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Only testing unsupported OS behavior")
	}

	inst := New(false)
	err := inst.RemoveDaemonService()
	if err == nil {
		t.Error("expected error on unsupported OS")
	}
}

func TestInstaller_SetupDaemonService_Fields(t *testing.T) {
	// Test that the function returns proper result structure
	inst := New(false)

	// Use a fake binary path - this won't actually install
	result := inst.SetupDaemonService(context.Background(), "/nonexistent/path")

	// Should have Dependency field set
	if result.Dependency != "Daemon Service" {
		t.Errorf("expected Dependency to be 'Daemon Service', got %s", result.Dependency)
	}
}
