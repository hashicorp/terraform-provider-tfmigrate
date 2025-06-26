// // Copyright (c) HashiCorp, Inc.
// // SPDX-License-Identifier: BUSL-1.1

package rpcapi

import (
	"context"
	"fmt"
	"os/exec"
	"terraform-provider-tfmigrate/internal/diagnostics"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	sev "github.com/hashicorp/terraform/tfdiags"

	"terraform-provider-tfmigrate/internal/terraform/rpcapi/terraform1/dependencies"
	"terraform-provider-tfmigrate/internal/terraform/rpcapi/terraform1/packages"
	"terraform-provider-tfmigrate/internal/terraform/rpcapi/terraform1/setup"
	"terraform-provider-tfmigrate/internal/terraform/rpcapi/terraform1/stacks"
)

var (
	_ Client = (*grpcClient)(nil)
)

type Client interface {
	Dependencies() dependencies.DependenciesClient
	Stacks() stacks.StacksClient
	Packages() packages.PackagesClient
	Stop()
}

func Connect(ctx context.Context, logger hclog.Logger, terraform string) (Client, diagnostics.Diagnostics) {
	var diags diagnostics.Diagnostics

	cmd := exec.CommandContext(ctx, terraform, "rpcapi")
	cmd.Dir = "."

	config := &plugin.ClientConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  1,
			MagicCookieKey:   "TERRAFORM_RPCAPI_COOKIE",
			MagicCookieValue: "fba0991c9bcd453982f0d88e2da95940",
		},
		Cmd:              cmd,
		AutoMTLS:         true,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		Managed:          false,
		Plugins: map[string]plugin.Plugin{
			"terraform": &TerraformPlugin{},
		},
		Logger: logger,
	}

	client := plugin.NewClient(config)
	if _, err := client.Start(); err != nil {
		diags = diags.Append(diagnostics.Sourceless(sev.Error, "Failed to start Terraform plugin", err.Error()))
		diags = diags.Append(diagnostics.Sourceless(sev.Warning, UnsupportedTerraformVersionError, ""))
		return nil, diags
	}

	protocol, err := client.Client()
	if err != nil {
		diags = diags.Append(diagnostics.Sourceless(sev.Error, "Failed to retrieve Terraform client", err.Error()))
		return nil, diags
	}

	raw, err := protocol.Dispense("terraform")
	if err != nil {
		diags = diags.Append(diagnostics.Sourceless(sev.Error, "Failed to create Terraform instance", err.Error()))
		return nil, diags
	}

	grpcClient := raw.(*grpcClient)
	grpcClient.pluginClient = client
	return grpcClient, diags
}

type grpcClient struct {
	pluginClient *plugin.Client

	conn         *grpc.ClientConn
	dependencies dependencies.DependenciesClient
	stacks       stacks.StacksClient
	packages     packages.PackagesClient
}

func (g *grpcClient) Dependencies() dependencies.DependenciesClient {
	if g.dependencies == nil {
		g.dependencies = dependencies.NewDependenciesClient(g.conn)
	}
	return g.dependencies
}

func (g *grpcClient) Stacks() stacks.StacksClient {
	if g.stacks == nil {
		g.stacks = stacks.NewStacksClient(g.conn)
	}
	return g.stacks
}

func (g *grpcClient) Packages() packages.PackagesClient {
	if g.packages == nil {
		g.packages = packages.NewPackagesClient(g.conn)
	}
	return g.packages
}

func (g *grpcClient) Stop() {
	g.pluginClient.Kill()
}

type TerraformPlugin struct {
	plugin.NetRPCUnsupportedPlugin
}

var (
	_ plugin.Plugin     = (*TerraformPlugin)(nil)
	_ plugin.GRPCPlugin = (*TerraformPlugin)(nil)
)

func (t *TerraformPlugin) GRPCServer(_ *plugin.GRPCBroker, _ *grpc.Server) error {
	// Nowhere in this codebase should we try and launch a server anyway.
	return fmt.Errorf("stacks only supports client gRPC connections")
}

func (t *TerraformPlugin) GRPCClient(ctx context.Context, _ *plugin.GRPCBroker, conn *grpc.ClientConn) (interface{}, error) {
	client := setup.NewSetupClient(conn)
	_, err := client.Handshake(ctx, &setup.Handshake_Request{})
	if err != nil {
		return nil, fmt.Errorf("rpcapi setup handshake failed: %v", err)
	}

	return &grpcClient{
		conn: conn,
	}, nil
}

const UnsupportedTerraformVersionError = `
The Terraform Stacks is only compatible with specific Terraform versions.

For supported Terraform versions, refer to: https://hashi.co/tfstacks-requirements
`
