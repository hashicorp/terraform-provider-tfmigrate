package git

import (
	"context"
	"os"
	"testing"
)

func TestBitbucketUtil_ValidateToken(t *testing.T) {
	ctx := context.Background()
	util := NewBitbucketUtil(ctx)

	os.Setenv("TF_BITBUCKET_TOKEN", "test-token")
	err := util.ValidateToken()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	os.Unsetenv("TF_BITBUCKET_TOKEN")
	err = util.ValidateToken()
	if err == nil {
		t.Errorf("expected error for unset token, got nil")
	}
}

// Add more tests for CreateCommit and CreatePullRequest as needed.
