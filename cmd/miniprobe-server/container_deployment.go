package main

import (
	"os"
	"strings"
)

// The container deployment marker only affects *local SSH management*.
// It does not enable public management endpoints or change Agent policies.
func isDockerDeployment() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("MINIPROBE_DEPLOYMENT")), "docker")
}
