package models

import "time"

type StackDeploymentStep struct {
	Id         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		Status            string    `json:"status"`
		OperationType     string    `json:"operation-type"`
		RequiresStateLock bool      `json:"requires-state-lock"`
		CreatedAt         time.Time `json:"created-at"`
		UpdatedAt         time.Time `json:"updated-at"`
	} `json:"attributes"`
	Links map[string]interface{} `jsonapi:"links,omitempty"`
}

type StackDeploymentSteps struct {
	Data []StackDeploymentStep `json:"data"`
}
