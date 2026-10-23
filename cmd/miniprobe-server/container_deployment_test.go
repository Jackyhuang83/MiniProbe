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

func TestDockerNetworkAccessMenuDoesNotCallLocalConfig(t *testing.T) {
	t.Setenv("MINIPROBE_DEPLOYMENT", "docker")
	reader := bufio.NewReader(strings.NewReader("1\n"))
	// No localClient is passed deliberately: the Docker early-return guard
	// must execute before any Unix-socket read or privileged host operation.
	manageNetworkAccess(reader, nil, "")
}

