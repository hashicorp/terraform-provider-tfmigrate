package models

import "time"

type StackDeploymentRun struct {
	Id            string                           `json:"id"`
	Type          string                           `json:"type"`
	Attributes    StackDeploymentRunAttributes     `json:"attributes"`
	Relationships *StackDeploymentRunRelationships `json:"relationships"`
}

type StackDeploymentRunAttributes struct {
	Status     string    `json:"status"`
	Deployment string    `json:"deployment"`
	CreatedAt  time.Time `json:"created-at"`
	UpdatedAt  time.Time `json:"updated-at"`
	PlanMode   string    `json:"plan-mode"`
}

type StackDeploymentRunRelationships struct {
	StackDeploymentGroup *StackDeploymentRunRelationship `json:"stack-deployment-group,omitempty"`
	StackConfiguration   *StackDeploymentRunRelationship `json:"stack-configuration,omitempty"`
	CurrentStep          *StackDeploymentRunRelationship `json:"current-step,omitempty"`
}

type StackDeploymentRunRelationship struct {
	Data *StackDeploymentRunRelationshipData `json:"data"`
}

type StackDeploymentRunRelationshipData struct {
	Id   string `json:"id"`
	Type string `json:"type"`
}

type StackDeploymentRuns struct {
	Data []StackDeploymentRun `json:"data"`
}
