package rpcapi

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"terraform-provider-tfmigrate/internal/constants"
	"time"
)

// StartTFRPCServer starts the Terraform RPC API server and returns a function to stop it.
func StartTFRPCServer() (stopTFRPCAPIServer func(), err error) {
	cmd := exec.Command(fmt.Sprintf("%s=%s", constants.TerraformMagicCookieKey, constants.TerraformRPCAPICookie), "terraform", "rpcapi")

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	time.Sleep(2 * time.Second) // naive wait

	stopTFRPCAPIServer = func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}

	if err := cmd.Process.Signal(os.Signal(nil)); err != nil && !errors.Is(err, os.ErrInvalid) {
		stopTFRPCAPIServer()
		return nil, errors.New("rpcapi process exited unexpectedly")
	}

	return stopTFRPCAPIServer, nil
}
