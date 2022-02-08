// Copyright 2020 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package doctor

import (
	"context"
	"fmt"
	"strings"

	"code.gitea.io/gitea/models"
	"code.gitea.io/gitea/models/db"
	repo_model "code.gitea.io/gitea/models/repo"
	"code.gitea.io/gitea/modules/git"
	"code.gitea.io/gitea/modules/log"

	"xorm.io/builder"
)

func iteratePRs(repo *repo_model.Repository, each func(*repo_model.Repository, *models.PullRequest) error) error {
	return db.Iterate(
		db.DefaultContext,
		new(models.PullRequest),
		builder.Eq{"base_repo_id": repo.ID},
		func(idx int, bean interface{}) error {
			return each(repo, bean.(*models.PullRequest))
		},
	)
}

func checkPRMergeBase(ctx context.Context, logger log.Logger, autofix bool) error {
	numRepos := 0
	numPRs := 0
	numPRsUpdated := 0
	err := iterateRepositories(func(repo *repo_model.Repository) error {
		numRepos++
		return iteratePRs(repo, func(repo *repo_model.Repository, pr *models.PullRequest) error {
			numPRs++
			pr.BaseRepo = repo
			repoPath := repo.RepoPath()

			oldMergeBase := pr.MergeBase

			if !pr.HasMerged {
				var err error
				pr.MergeBase, err = git.NewCommand(ctx, "merge-base", "--", pr.BaseBranch, pr.GetGitRefName()).RunInDir(repoPath)
				if err != nil {
					var err2 error
					pr.MergeBase, err2 = git.NewCommand(ctx, "rev-parse", git.BranchPrefix+pr.BaseBranch).RunInDir(repoPath)
					if err2 != nil {
						logger.Warn("Unable to get merge base for PR ID %d, #%d onto %s in %s/%s. Error: %v & %v", pr.ID, pr.Index, pr.BaseBranch, pr.BaseRepo.OwnerName, pr.BaseRepo.Name, err, err2)
						return nil
					}
				}
			} else {
				parentsString, err := git.NewCommand(ctx, "rev-list", "--parents", "-n", "1", pr.MergedCommitID).RunInDir(repoPath)
				if err != nil {
					logger.Warn("Unable to get parents for merged PR ID %d, #%d onto %s in %s/%s. Error: %v", pr.ID, pr.Index, pr.BaseBranch, pr.BaseRepo.OwnerName, pr.BaseRepo.Name, err)
					return nil
				}
				parents := strings.Split(strings.TrimSpace(parentsString), " ")
				if len(parents) < 2 {
					return nil
				}

				args := append([]string{"merge-base", "--"}, parents[1:]...)
				args = append(args, pr.GetGitRefName())

				pr.MergeBase, err = git.NewCommand(ctx, args...).RunInDir(repoPath)
				if err != nil {
					logger.Warn("Unable to get merge base for merged PR ID %d, #%d onto %s in %s/%s. Error: %v", pr.ID, pr.Index, pr.BaseBranch, pr.BaseRepo.OwnerName, pr.BaseRepo.Name, err)
					return nil
				}
			}
			pr.MergeBase = strings.TrimSpace(pr.MergeBase)
			if pr.MergeBase != oldMergeBase {
				if autofix {
					if err := pr.UpdateCols("merge_base"); err != nil {
						logger.Critical("Failed to update merge_base. ERROR: %v", err)
						return fmt.Errorf("Failed to update merge_base. ERROR: %v", err)
					}
				} else {
					logger.Info("#%d onto %s in %s/%s: MergeBase should be %s but is %s", pr.Index, pr.BaseBranch, pr.BaseRepo.OwnerName, pr.BaseRepo.Name, oldMergeBase, pr.MergeBase)
				}
				numPRsUpdated++
			}
			return nil
		})
	})

	if autofix {
		logger.Info("%d PR mergebases updated of %d PRs total in %d repos", numPRsUpdated, numPRs, numRepos)
	} else {
		if numPRsUpdated > 0 && err == nil {
			logger.Critical("%d PRs with incorrect mergebases of %d PRs total in %d repos", numPRsUpdated, numPRs, numRepos)
			return fmt.Errorf("%d PRs with incorrect mergebases of %d PRs total in %d repos", numPRsUpdated, numPRs, numRepos)
		}

		logger.Warn("%d PRs with incorrect mergebases of %d PRs total in %d repos", numPRsUpdated, numPRs, numRepos)
	}

	return err
}

func init() {
	Register(&Check{
		Title:     "Recalculate merge bases",
		Name:      "recalculate-merge-bases",
		IsDefault: false,
		Run:       checkPRMergeBase,
		Priority:  7,
	})
}
