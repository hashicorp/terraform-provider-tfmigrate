package stack

import "github.com/hashicorp/go-tfe"

const (
	CurrentStackConfigIsNotValid             = "The stack %q in organization %q and project %q has an invalid latest stack configuration state. The configuration ID or status is empty."
	StackDeploymentRunApiPathTemplate        = "%s%sstacks/%s/stack-deployments/%s/stack-deployment-runs"
	StackDploymentsByConfigIdApiPathTemplate = "%s%sstacks/%s/stack-deployments?filter[stack_configuration][id]=%s"
)

var (
	RunningDeploymentGroupStatuses = []tfe.DeploymentGroupStatus{
		tfe.DeploymentGroupStatusPending,
		tfe.DeploymentGroupStatusDeploying,
	}

	RunningStackConfigurationStatuses = []tfe.StackConfigurationStatus{
		tfe.StackConfigurationStatusPending,
		tfe.StackConfigurationStatusQueued,
		tfe.StackConfigurationStatusPreparing,
	}

	ErroredOrCancelledStackConfigurationStatuses = []tfe.StackConfigurationStatus{
		tfe.StackConfigurationStatusFailed,
	}

	TerminalStackDeploymentGroupStatuses = []tfe.DeploymentGroupStatus{
		tfe.DeploymentGroupStatusFailed,
		tfe.DeploymentGroupStatusAbandoned,
		tfe.DeploymentGroupStatusSucceeded,
	}
)
