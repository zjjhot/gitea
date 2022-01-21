// Copyright 2019 The Gitea Authors.
// All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package pull

import (
	"context"

	"code.gitea.io/gitea/models"
	"code.gitea.io/gitea/models/db"
	"code.gitea.io/gitea/modules/git"
	"code.gitea.io/gitea/modules/structs"

	"github.com/pkg/errors"
)

// MergeRequiredContextsCommitStatus returns a commit status state for given required contexts
func MergeRequiredContextsCommitStatus(commitStatuses []*models.CommitStatus, requiredContexts []string) structs.CommitStatusState {
	if len(requiredContexts) == 0 {
		status := models.CalcCommitStatus(commitStatuses)
		if status != nil {
			return status.State
		}
		return structs.CommitStatusSuccess
	}

	returnedStatus := structs.CommitStatusSuccess
	for _, ctx := range requiredContexts {
		var targetStatus structs.CommitStatusState
		for _, commitStatus := range commitStatuses {
			if commitStatus.Context == ctx {
				targetStatus = commitStatus.State
				break
			}
		}

		if targetStatus == "" {
			targetStatus = structs.CommitStatusPending
			commitStatuses = append(commitStatuses, &models.CommitStatus{
				State:       targetStatus,
				Context:     ctx,
				Description: "Pending",
			})
		}
		if targetStatus.NoBetterThan(returnedStatus) {
			returnedStatus = targetStatus
		}
	}
	return returnedStatus
}

// IsCommitStatusContextSuccess returns true if all required status check contexts succeed.
func IsCommitStatusContextSuccess(commitStatuses []*models.CommitStatus, requiredContexts []string) bool {
	// If no specific context is required, require that last commit status is a success
	if len(requiredContexts) == 0 {
		status := models.CalcCommitStatus(commitStatuses)
		if status == nil || status.State != structs.CommitStatusSuccess {
			return false
		}
		return true
	}

	for _, ctx := range requiredContexts {
		var found bool
		for _, commitStatus := range commitStatuses {
			if commitStatus.Context == ctx {
				if commitStatus.State != structs.CommitStatusSuccess {
					return false
				}

				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// IsPullCommitStatusPass returns if all required status checks PASS
func IsPullCommitStatusPass(ctx context.Context, pr *models.PullRequest) (bool, error) {
	if err := pr.LoadProtectedBranch(); err != nil {
		return false, errors.Wrap(err, "GetLatestCommitStatus")
	}
	if pr.ProtectedBranch == nil || !pr.ProtectedBranch.EnableStatusCheck {
		return true, nil
	}

	state, err := GetPullRequestCommitStatusState(ctx, pr)
	if err != nil {
		return false, err
	}
	return state.IsSuccess(), nil
}

// GetPullRequestCommitStatusState returns pull request merged commit status state
func GetPullRequestCommitStatusState(ctx context.Context, pr *models.PullRequest) (structs.CommitStatusState, error) {
	// Ensure HeadRepo is loaded
	if err := pr.LoadHeadRepo(); err != nil {
		return "", errors.Wrap(err, "LoadHeadRepo")
	}

	// check if all required status checks are successful
	headGitRepo, closer, err := git.RepositoryFromContextOrOpen(ctx, pr.HeadRepo.RepoPath())
	if err != nil {
		return "", errors.Wrap(err, "OpenRepository")
	}
	defer closer.Close()

	if pr.Flow == models.PullRequestFlowGithub && !headGitRepo.IsBranchExist(pr.HeadBranch) {
		return "", errors.New("Head branch does not exist, can not merge")
	}
	if pr.Flow == models.PullRequestFlowAGit && !git.IsReferenceExist(headGitRepo.Ctx, headGitRepo.Path, pr.GetGitRefName()) {
		return "", errors.New("Head branch does not exist, can not merge")
	}

	var sha string
	if pr.Flow == models.PullRequestFlowGithub {
		sha, err = headGitRepo.GetBranchCommitID(pr.HeadBranch)
	} else {
		sha, err = headGitRepo.GetRefCommitID(pr.GetGitRefName())
	}
	if err != nil {
		return "", err
	}

	if err := pr.LoadBaseRepo(); err != nil {
		return "", errors.Wrap(err, "LoadBaseRepo")
	}

	commitStatuses, _, err := models.GetLatestCommitStatus(pr.BaseRepo.ID, sha, db.ListOptions{})
	if err != nil {
		return "", errors.Wrap(err, "GetLatestCommitStatus")
	}

	return MergeRequiredContextsCommitStatus(commitStatuses, pr.ProtectedBranch.StatusCheckContexts), nil
}
