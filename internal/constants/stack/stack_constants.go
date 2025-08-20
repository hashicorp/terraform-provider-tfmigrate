package stack

const (
	UnknownStackPlanUpdateStrategy    StackPlanUpdateStrategy = "unknown"
	RetainPlanStackPlanUpdateStrategy StackPlanUpdateStrategy = "no_modification"
	ModifyPlanStackPlanUpdateStrategy StackPlanUpdateStrategy = "modify_plan"

	StackDeploymentRunApiPathTemplate = "%s%s/stacks/%s/stack-deployments/%s/stack-deployment-runs"
)

type StackPlanUpdateStrategy string // StackPlanUpdateStrategy represents the strategy for updating a stack plan.

func (s StackPlanUpdateStrategy) String() string {
	return string(s)
}
