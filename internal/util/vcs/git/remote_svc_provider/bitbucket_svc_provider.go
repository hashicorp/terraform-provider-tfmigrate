package remote_svc_provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"terraform-provider-tfmigrate/internal/util/vcs/git"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/ktrysmt/go-bitbucket"
)

// BitbucketSvcProvider is used to interact with the Bitbucket API
type BitbucketSvcProvider struct {
	client *http.Client
	ctx    context.Context
}

// NewBitbucketSvcProvider creates a new Bitbucket service provider
func NewBitbucketSvcProvider(ctx context.Context) *BitbucketSvcProvider {
	return &BitbucketSvcProvider{
		client: &http.Client{},
		ctx:    ctx,
	}
}

// ValidateToken checks if the Bitbucket token is set and valid
func (b *BitbucketSvcProvider) ValidateToken() error {
	token, isSet := os.LookupEnv("TF_BITBUCKET_TOKEN")
	if !isSet || token == "" {
		return fmt.Errorf("TF_BITBUCKET_TOKEN not set or empty")
	}
	return nil
}

// CreateCommit creates a new commit in the specified repository
func (b *BitbucketSvcProvider) CreateCommit(owner, repo, branch, message, filePath, content string) error {
	token := os.Getenv("TF_BITBUCKET_TOKEN")
	url := fmt.Sprintf("https://api.bitbucket.org/2.0/repositories/%s/%s/src", owner, repo)
	body := &bytes.Buffer{}
	writer := json.NewEncoder(body)
	payload := map[string]interface{}{
		"branch":  branch,
		"message": message,
		filePath:  content,
	}
	if err := writer.Encode(payload); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(b.ctx, "POST", url, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("failed to create commit: %s", resp.Status)
	}
	return nil
}

// bitbucketSvcProvider implements RemoteVcsSvcProvider for Bitbucket
var _ RemoteVcsSvcProvider = (*bitbucketSvcProvider)(nil)

type bitbucketSvcProvider struct {
	ctx           context.Context
	git           git.GitUtil
	bitbucketUtil git.BitbucketUtil
}

func (b *bitbucketSvcProvider) ValidateToken(repoUrl string, repoIdentifier string, tokenFromProvider string) (string, error) {
	if err := b.bitbucketUtil.ValidateToken(); err != nil {
		return "", err
	}
	return "Bitbucket token is valid", nil
}

// CreatePullRequest creates a pull request on the Bitbucket repository.
func (b *bitbucketSvcProvider) CreatePullRequest(params git.PullRequestParams) (string, error) {
	// Bitbucket expects repoIdentifier as "owner/repo"
	parts := strings.Split(params.RepoIdentifier, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid repository identifier. It should be in the format: owner/repository")
	}
	owner := parts[0]
	repo := parts[1]

	// Create bitbucket client with the token
	client := bitbucket.NewBasicAuth("", params.GitPatToken)

	// Create the pull request
	pr, err := client.Repositories.PullRequests.Create(&bitbucket.PullRequestsOptions{
		Owner:             owner,
		RepoSlug:          repo,
		Title:             params.Title,
		Description:       params.Body,
		SourceBranch:      params.FeatureBranch,
		DestinationBranch: params.BaseBranch,
		CloseSourceBranch: false,
	})

	if err != nil {
		tflog.Error(b.ctx, "Failed to create pull request", map[string]interface{}{
			"owner": owner,
			"repo":  repo,
			"title": params.Title,
			"error": err.Error(),
		})
		return "", fmt.Errorf("failed to create pull request: %v", err)
	}

	// Extract the HTML URL from the response
	if links, ok := pr.(map[string]interface{})["links"].(map[string]interface{}); ok {
		if html, ok := links["html"].(map[string]interface{}); ok {
			if href, ok := html["href"].(string); ok {
				return href, nil
			}
		}
	}

	return "", fmt.Errorf("could not extract pull request URL from response")
}
