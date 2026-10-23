package main

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

func TestDockerDeploymentMarker(t *testing.T) {
	for _, tc := range []struct{ raw string; want bool }{
		{"", false}, {"systemd", false}, {"docker", true}, {" Docker ", true}, {"false", false},
	} {
		t.Setenv("MINIPROBE_DEPLOYMENT", tc.raw)
		if got := isDockerDeployment(); got != tc.want {
			t.Fatalf("marker %q: got %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestDockerUpdateMenuDoesNotAccessNetworkOrChangeHost(t *testing.T) {
	t.Setenv("MINIPROBE_DEPLOYMENT", "docker")
	// The Docker path must return without touching the local admin socket or
	// calling GitHub; checking it with an unavailable socket is sufficient.
	reader := bufio.NewReader(strings.NewReader("YES\n"))
	if manageServerUpdate(reader, "/does-not-exist/miniprobe.sock") {
		t.Fatal("Docker mode attempted to upgrade the host")
	}
}

func TestDockerMenu13RequestsOnlyHostSideUpdate(t *testing.T) {
	t.Setenv("MINIPROBE_DEPLOYMENT", "docker")
	reader := bufio.NewReader(strings.NewReader("YES\n"))
	finished, err := dispatchServerUpdateMenu(reader, "/socket-does-not-exist")
	if finished || !errors.Is(err, errDockerUpgradeRequested) {
		t.Fatalf("unexpected Docker menu action: finished=%v, err=%v", finished, err)
	}
	if dockerUpgradeRequestExitCode != 73 {
		t.Fatalf("unexpected handoff code: %d", dockerUpgradeRequestExitCode)
	}
}

func TestDockerMenu8DelegatesOnlyFixedActions(t *testing.T) {
	t.Setenv("MINIPROBE_DEPLOYMENT", "docker")
	for _, tc := range []struct {
		input string
		want error
	}{
		{"1\n", errDockerTunnelStatusRequested},
		{"2\n", errDockerTunnelStartRequested},
		{"3\n", errDockerTunnelStopRequested},
		{"0\n", nil},
		{"4\n", nil},
	} {
		got := dispatchNetworkAccessMenu(bufio.NewReader(strings.NewReader(tc.input)), nil, "/not-present.sock")
		if !errors.Is(got, tc.want) {
			t.Fatalf("menu 8 option %q: got %v, want %v", tc.input, got, tc.want)
		}
	}
	for _, code := range []int{dockerTunnelStatusExitCode, dockerTunnelStartExitCode, dockerTunnelStopExitCode} {
		if code == 0 || code == dockerUpgradeRequestExitCode {
			t.Fatalf("reserved menu handoff code conflict: %d", code)
		}
	}
}

func TestDockerNetworkAccessMenuDoesNotCallLocalConfig(t *testing.T) {
	t.Setenv("MINIPROBE_DEPLOYMENT", "docker")
	reader := bufio.NewReader(strings.NewReader("1\n"))
	// No localClient is passed deliberately: the Docker early-return guard
	// must execute before any Unix-socket read or privileged host operation.
	manageNetworkAccess(reader, nil, "")
}

