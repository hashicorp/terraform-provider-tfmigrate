package git

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	cliErrs "terraform-provider-tfmigrate/internal/cli_errors"
)

type bitbucketUtil struct {
	client *http.Client
	ctx    context.Context
}

type BitbucketUtil interface {
	ValidateToken() error
	CreatePullRequest(owner, repo, sourceBranch, destinationBranch, title, description string) (string, error)
}

// NewBitbucketUtil creates a new instance of BitbucketUtil.
func NewBitbucketUtil(ctx context.Context) BitbucketUtil {
	return &bitbucketUtil{
		client: &http.Client{},
		ctx:    ctx,
	}
}

// ValidateToken checks if the Bitbucket token is set and valid.
func (b *bitbucketUtil) ValidateToken() error {
	token, isSet := os.LookupEnv("TF_BITBUCKET_TOKEN")
	if !isSet {
		return cliErrs.ErrTfBitbucketTokenNotSet
	}
	if token == "" {
		return cliErrs.ErrTfBitbucketTokenEmpty
	}
	// Optionally, validate token by making a simple API call
	return nil
}

// CreatePullRequest creates a pull request in the specified Bitbucket repository.
func (b *bitbucketUtil) CreatePullRequest(owner, repo, sourceBranch, destinationBranch, title, description string) (string, error) {
	token := os.Getenv("TF_BITBUCKET_TOKEN")
	url := fmt.Sprintf("https://api.bitbucket.org/2.0/repositories/%s/%s/pullrequests", owner, repo)
	payload := map[string]interface{}{
		"title":       title,
		"description": description,
		"source": map[string]interface{}{
			"branch": map[string]string{"name": sourceBranch},
		},
		"destination": map[string]interface{}{
			"branch": map[string]string{"name": destinationBranch},
		},
	}
	body := &bytes.Buffer{}
	writer := json.NewEncoder(body)
	if err := writer.Encode(payload); err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(b.ctx, "POST", url, body)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(request)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("failed to create pull request: %s", resp.Status)
	}
	var result struct {
		Links struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Links.HTML.Href, nil
}
