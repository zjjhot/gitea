// Copyright 2019 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package indexer

import (
	"code.gitea.io/gitea/models"
	repo_model "code.gitea.io/gitea/models/repo"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/git"
	code_indexer "code.gitea.io/gitea/modules/indexer/code"
	issue_indexer "code.gitea.io/gitea/modules/indexer/issues"
	stats_indexer "code.gitea.io/gitea/modules/indexer/stats"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/notification/base"
	"code.gitea.io/gitea/modules/repository"
	"code.gitea.io/gitea/modules/setting"
)

type indexerNotifier struct {
	base.NullNotifier
}

var _ base.Notifier = &indexerNotifier{}

// NewNotifier create a new indexerNotifier notifier
func NewNotifier() base.Notifier {
	return &indexerNotifier{}
}

func (r *indexerNotifier) NotifyCreateIssueComment(doer *user_model.User, repo *repo_model.Repository,
	issue *models.Issue, comment *models.Comment, mentions []*user_model.User) {
	if comment.Type == models.CommentTypeComment {
		if issue.Comments == nil {
			if err := issue.LoadDiscussComments(); err != nil {
				log.Error("LoadComments failed: %v", err)
				return
			}
		} else {
			issue.Comments = append(issue.Comments, comment)
		}

		issue_indexer.UpdateIssueIndexer(issue)
	}
}

func (r *indexerNotifier) NotifyNewIssue(issue *models.Issue, mentions []*user_model.User) {
	issue_indexer.UpdateIssueIndexer(issue)
}

func (r *indexerNotifier) NotifyNewPullRequest(pr *models.PullRequest, mentions []*user_model.User) {
	issue_indexer.UpdateIssueIndexer(pr.Issue)
}

func (r *indexerNotifier) NotifyUpdateComment(doer *user_model.User, c *models.Comment, oldContent string) {
	if c.Type == models.CommentTypeComment {
		var found bool
		if c.Issue.Comments != nil {
			for i := 0; i < len(c.Issue.Comments); i++ {
				if c.Issue.Comments[i].ID == c.ID {
					c.Issue.Comments[i] = c
					found = true
					break
				}
			}
		}

		if !found {
			if err := c.Issue.LoadDiscussComments(); err != nil {
				log.Error("LoadComments failed: %v", err)
				return
			}
		}

		issue_indexer.UpdateIssueIndexer(c.Issue)
	}
}

func (r *indexerNotifier) NotifyDeleteComment(doer *user_model.User, comment *models.Comment) {
	if comment.Type == models.CommentTypeComment {
		if err := comment.LoadIssue(); err != nil {
			log.Error("LoadIssue: %v", err)
			return
		}

		var found bool
		if comment.Issue.Comments != nil {
			for i := 0; i < len(comment.Issue.Comments); i++ {
				if comment.Issue.Comments[i].ID == comment.ID {
					comment.Issue.Comments = append(comment.Issue.Comments[:i], comment.Issue.Comments[i+1:]...)
					found = true
					break
				}
			}
		}

		if !found {
			if err := comment.Issue.LoadDiscussComments(); err != nil {
				log.Error("LoadComments failed: %v", err)
				return
			}
		}
		// reload comments to delete the old comment
		issue_indexer.UpdateIssueIndexer(comment.Issue)
	}
}

func (r *indexerNotifier) NotifyDeleteRepository(doer *user_model.User, repo *repo_model.Repository) {
	issue_indexer.DeleteRepoIssueIndexer(repo)
	if setting.Indexer.RepoIndexerEnabled {
		code_indexer.UpdateRepoIndexer(repo)
	}
}

func (r *indexerNotifier) NotifyMigrateRepository(doer, u *user_model.User, repo *repo_model.Repository) {
	issue_indexer.UpdateRepoIndexer(repo)
	if setting.Indexer.RepoIndexerEnabled && !repo.IsEmpty {
		code_indexer.UpdateRepoIndexer(repo)
	}
	if err := stats_indexer.UpdateRepoIndexer(repo); err != nil {
		log.Error("stats_indexer.UpdateRepoIndexer(%d) failed: %v", repo.ID, err)
	}
}

func (r *indexerNotifier) NotifyPushCommits(pusher *user_model.User, repo *repo_model.Repository, opts *repository.PushUpdateOptions, commits *repository.PushCommits) {
	if setting.Indexer.RepoIndexerEnabled && opts.RefFullName == git.BranchPrefix+repo.DefaultBranch {
		code_indexer.UpdateRepoIndexer(repo)
	}
	if err := stats_indexer.UpdateRepoIndexer(repo); err != nil {
		log.Error("stats_indexer.UpdateRepoIndexer(%d) failed: %v", repo.ID, err)
	}
}

func (r *indexerNotifier) NotifySyncPushCommits(pusher *user_model.User, repo *repo_model.Repository, opts *repository.PushUpdateOptions, commits *repository.PushCommits) {
	if setting.Indexer.RepoIndexerEnabled && opts.RefFullName == git.BranchPrefix+repo.DefaultBranch {
		code_indexer.UpdateRepoIndexer(repo)
	}
	if err := stats_indexer.UpdateRepoIndexer(repo); err != nil {
		log.Error("stats_indexer.UpdateRepoIndexer(%d) failed: %v", repo.ID, err)
	}
}

func (r *indexerNotifier) NotifyIssueChangeContent(doer *user_model.User, issue *models.Issue, oldContent string) {
	issue_indexer.UpdateIssueIndexer(issue)
}

func (r *indexerNotifier) NotifyIssueChangeTitle(doer *user_model.User, issue *models.Issue, oldTitle string) {
	issue_indexer.UpdateIssueIndexer(issue)
}

func (r *indexerNotifier) NotifyIssueChangeRef(doer *user_model.User, issue *models.Issue, oldRef string) {
	issue_indexer.UpdateIssueIndexer(issue)
}
