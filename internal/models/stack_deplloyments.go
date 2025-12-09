package models

type StackDeploymentLinks struct {
	Self                string `json:"self"`
	StackDeploymentRuns string `json:"stack-deployment-runs"`
}

type StackDeploymentRelationShips struct {
	Stack struct {
		Data struct {
			Id   string `json:"id"`
			Type string `json:"type"`
		} `json:"data"`
	} `json:"stack"`
	LatestDeploymentRun struct {
		Data struct {
			Id   string `json:"id"`
			Type string `json:"type"`
		} `json:"data"`
	} `json:"latest-deployment-run"`
}

type StackDeploymentAttributes struct {
	Name string `json:"name"`
}

type StackDeploymentsMeta struct {
	Pagination `json:"pagination"`
}

type StackDeploymentData struct {
	Id            string                       `json:"id"`
	Type          string                       `json:"type"`
	Attributes    StackDeploymentAttributes    `json:"attributes"`
	Relationships StackDeploymentRelationShips `json:"relationships"`
	Links         StackDeploymentLinks         `json:"links"`
}

type StackDeployments struct {
	Data []StackDeploymentData `json:"data"`
	Meta StackDeploymentsMeta  `json:"meta"`
}
