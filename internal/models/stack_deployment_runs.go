package models

import "time"

type StackDeploymentRun struct {
	Id         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		Status     string    `json:"status"`
		Deployment string    `json:"deployment"`
		CreatedAt  time.Time `json:"created-at"`
		UpdatedAt  time.Time `json:"updated-at"`
		PlanMode   string    `json:"plan-mode"`
	} `json:"attributes"`
	Relationships struct {
		StackDeploymentGroup struct {
			Data struct {
				Id   string `json:"id"`
				Type string `json:"type"`
			} `json:"data"`
		} `json:"stack-deployment-group"`

		StackConfiguration struct {
			Data struct {
				Id   string `json:"id"`
				Type string `json:"type"`
			} `json:"data"`
		} `json:"stack-configuration"`

		CurrentStep struct {
			Data struct {
				Id   string `json:"id"`
				Type string `json:"type"`
			} `json:"data"`
		} `json:"current-step"`
	} `json:"relationships"`
}

type StackDeploymentRuns struct {
	Data []StackDeploymentRun `json:"data"`
}
