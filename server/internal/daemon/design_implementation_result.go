package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
	"github.com/multica-ai/multica/server/internal/designimplementation"
)

func collectDesignImplementationReceipt(task Task, workDir string, now time.Time) (*designimplementation.Receipt, error) {
	identity, ok := designimplementation.ParseTaskIdentity(task.TriggerCommentContent)
	if !ok {
		return nil, nil
	}
	repositoryDir, err := designImplementationRepositoryDir(task, workDir, identity)
	if err != nil {
		return nil, err
	}
	receipt, err := designimplementation.CollectReceiptFromRepository(workDir, repositoryDir, now)
	if err != nil {
		return nil, err
	}
	if !designImplementationReceiptMatchesTask(task, receipt.Identity, identity) {
		return nil, errors.New("design implementation receipt does not match the dispatched task identity")
	}
	return receipt, nil
}

func designImplementationReceiptMatchesTask(task Task, receipt designimplementation.FrozenIdentity, identity designimplementation.TaskIdentity) bool {
	return receipt.ProjectID == task.ProjectID && receipt.IssueID == task.IssueID &&
		receipt.ProjectResourceID == identity.ProjectResourceID && receipt.DesignRef == identity.DesignRef &&
		receipt.RevisionID == identity.RevisionID && receipt.ContentDigest == identity.ContentDigest &&
		slices.Equal(receipt.FrameRefs, identity.SelectedFrameRefs())
}

func designImplementationRepositoryDir(task Task, workDir string, identity designimplementation.TaskIdentity) (string, error) {
	if _, err := os.Stat(filepath.Join(workDir, ".git")); err == nil {
		return workDir, nil
	}

	var selectedURL string
	peerURLs := make([]string, 0, len(task.Repos))
	for _, repository := range task.Repos {
		if url := strings.TrimSpace(repository.URL); url != "" {
			peerURLs = append(peerURLs, url)
		}
	}
	for _, resource := range task.ProjectResources {
		if resource.ID != identity.ProjectResourceID || resource.ResourceType != "github_repo" {
			continue
		}
		var reference struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(resource.ResourceRef, &reference); err != nil {
			return "", fmt.Errorf("selected implementation repository is invalid: %w", err)
		}
		selectedURL = strings.TrimSpace(reference.URL)
		break
	}
	if selectedURL == "" {
		return "", errors.New("selected implementation repository is unavailable")
	}
	if len(peerURLs) == 0 {
		peerURLs = []string{selectedURL}
	}
	checkoutName := repocache.CheckoutDirName(selectedURL, peerURLs)
	repositoryDir := filepath.Join(workDir, checkoutName)
	if _, err := os.Stat(filepath.Join(repositoryDir, ".git")); err == nil {
		return repositoryDir, nil
	}
	reusedDesignDocumentDir := filepath.Join(workDir, "repositories", checkoutName)
	if _, err := os.Stat(filepath.Join(reusedDesignDocumentDir, ".git")); err == nil {
		return reusedDesignDocumentDir, nil
	}
	return repositoryDir, nil
}
