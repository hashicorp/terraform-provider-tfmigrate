package remote_svc_provider_mocks

import (
	"github.com/stretchr/testify/mock"
)

type MockBitbucketSvcProvider struct {
	mock.Mock
}

func (m *MockBitbucketSvcProvider) ValidateToken() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockBitbucketSvcProvider) CreateCommit(owner, repo, branch, message, filePath, content string) error {
	args := m.Called(owner, repo, branch, message, filePath, content)
	return args.Error(0)
}

func (m *MockBitbucketSvcProvider) CreatePullRequest(owner, repo, sourceBranch, destinationBranch, title, description string) (string, error) {
	args := m.Called(owner, repo, sourceBranch, destinationBranch, title, description)
	return args.String(0), args.Error(1)
}
