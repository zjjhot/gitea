// Copyright 2019 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package pull

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"code.gitea.io/gitea/models"
	"code.gitea.io/gitea/models/db"
	repo_model "code.gitea.io/gitea/models/repo"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/git"
	"code.gitea.io/gitea/modules/graceful"
	"code.gitea.io/gitea/modules/json"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/notification"
	"code.gitea.io/gitea/modules/process"
	"code.gitea.io/gitea/modules/setting"
	issue_service "code.gitea.io/gitea/services/issue"
)

// NewPullRequest creates new pull request with labels for repository.
func NewPullRequest(ctx context.Context, repo *repo_model.Repository, pull *models.Issue, labelIDs []int64, uuids []string, pr *models.PullRequest, assigneeIDs []int64) error {
	if err := TestPatch(pr); err != nil {
		return err
	}

	divergence, err := GetDiverging(ctx, pr)
	if err != nil {
		return err
	}
	pr.CommitsAhead = divergence.Ahead
	pr.CommitsBehind = divergence.Behind

	if err := models.NewPullRequest(ctx, repo, pull, labelIDs, uuids, pr); err != nil {
		return err
	}

	for _, assigneeID := range assigneeIDs {
		if err := issue_service.AddAssigneeIfNotAssigned(pull, pull.Poster, assigneeID); err != nil {
			return err
		}
	}

	pr.Issue = pull
	pull.PullRequest = pr

	// Now - even if the request context has been cancelled as the PR has been created
	// in the db and there is no way to cancel that transaction we have to proceed - therefore
	// create new context and work from there
	prCtx, _, finished := process.GetManager().AddContext(graceful.GetManager().HammerContext(), fmt.Sprintf("NewPullRequest: %s:%d", repo.FullName(), pr.Index))
	defer finished()

	if pr.Flow == models.PullRequestFlowGithub {
		err = PushToBaseRepo(prCtx, pr)
	} else {
		err = UpdateRef(prCtx, pr)
	}
	if err != nil {
		return err
	}

	mentions, err := pull.FindAndUpdateIssueMentions(db.DefaultContext, pull.Poster, pull.Content)
	if err != nil {
		return err
	}

	notification.NotifyNewPullRequest(pr, mentions)
	if len(pull.Labels) > 0 {
		notification.NotifyIssueChangeLabels(pull.Poster, pull, pull.Labels, nil)
	}
	if pull.Milestone != nil {
		notification.NotifyIssueChangeMilestone(pull.Poster, pull, 0)
	}

	// add first push codes comment
	baseGitRepo, err := git.OpenRepositoryCtx(prCtx, pr.BaseRepo.RepoPath())
	if err != nil {
		return err
	}
	defer baseGitRepo.Close()

	compareInfo, err := baseGitRepo.GetCompareInfo(pr.BaseRepo.RepoPath(),
		git.BranchPrefix+pr.BaseBranch, pr.GetGitRefName(), false, false)
	if err != nil {
		return err
	}

	if len(compareInfo.Commits) > 0 {
		data := models.PushActionContent{IsForcePush: false}
		data.CommitIDs = make([]string, 0, len(compareInfo.Commits))
		for i := len(compareInfo.Commits) - 1; i >= 0; i-- {
			data.CommitIDs = append(data.CommitIDs, compareInfo.Commits[i].ID.String())
		}

		dataJSON, err := json.Marshal(data)
		if err != nil {
			return err
		}

		ops := &models.CreateCommentOptions{
			Type:        models.CommentTypePullRequestPush,
			Doer:        pull.Poster,
			Repo:        repo,
			Issue:       pr.Issue,
			IsForcePush: false,
			Content:     string(dataJSON),
		}

		_, _ = models.CreateComment(ops)
	}

	return nil
}

// ChangeTargetBranch changes the target branch of this pull request, as the given user.
func ChangeTargetBranch(ctx context.Context, pr *models.PullRequest, doer *user_model.User, targetBranch string) (err error) {
	// Current target branch is already the same
	if pr.BaseBranch == targetBranch {
		return nil
	}

	if pr.Issue.IsClosed {
		return models.ErrIssueIsClosed{
			ID:     pr.Issue.ID,
			RepoID: pr.Issue.RepoID,
			Index:  pr.Issue.Index,
		}
	}

	if pr.HasMerged {
		return models.ErrPullRequestHasMerged{
			ID:         pr.ID,
			IssueID:    pr.Index,
			HeadRepoID: pr.HeadRepoID,
			BaseRepoID: pr.BaseRepoID,
			HeadBranch: pr.HeadBranch,
			BaseBranch: pr.BaseBranch,
		}
	}

	// Check if branches are equal
	branchesEqual, err := IsHeadEqualWithBranch(ctx, pr, targetBranch)
	if err != nil {
		return err
	}
	if branchesEqual {
		return models.ErrBranchesEqual{
			HeadBranchName: pr.HeadBranch,
			BaseBranchName: targetBranch,
		}
	}

	// Check if pull request for the new target branch already exists
	existingPr, err := models.GetUnmergedPullRequest(pr.HeadRepoID, pr.BaseRepoID, pr.HeadBranch, targetBranch, models.PullRequestFlowGithub)
	if existingPr != nil {
		return models.ErrPullRequestAlreadyExists{
			ID:         existingPr.ID,
			IssueID:    existingPr.Index,
			HeadRepoID: existingPr.HeadRepoID,
			BaseRepoID: existingPr.BaseRepoID,
			HeadBranch: existingPr.HeadBranch,
			BaseBranch: existingPr.BaseBranch,
		}
	}
	if err != nil && !models.IsErrPullRequestNotExist(err) {
		return err
	}

	// Set new target branch
	oldBranch := pr.BaseBranch
	pr.BaseBranch = targetBranch

	// Refresh patch
	if err := TestPatch(pr); err != nil {
		return err
	}

	// Update target branch, PR diff and status
	// This is the same as checkAndUpdateStatus in check service, but also updates base_branch
	if pr.Status == models.PullRequestStatusChecking {
		pr.Status = models.PullRequestStatusMergeable
	}

	// Update Commit Divergence
	divergence, err := GetDiverging(ctx, pr)
	if err != nil {
		return err
	}
	pr.CommitsAhead = divergence.Ahead
	pr.CommitsBehind = divergence.Behind

	if err := pr.UpdateColsIfNotMerged("merge_base", "status", "conflicted_files", "changed_protected_files", "base_branch", "commits_ahead", "commits_behind"); err != nil {
		return err
	}

	// Create comment
	options := &models.CreateCommentOptions{
		Type:   models.CommentTypeChangeTargetBranch,
		Doer:   doer,
		Repo:   pr.Issue.Repo,
		Issue:  pr.Issue,
		OldRef: oldBranch,
		NewRef: targetBranch,
	}
	if _, err = models.CreateComment(options); err != nil {
		return fmt.Errorf("CreateChangeTargetBranchComment: %v", err)
	}

	return nil
}

func checkForInvalidation(ctx context.Context, requests models.PullRequestList, repoID int64, doer *user_model.User, branch string) error {
	repo, err := repo_model.GetRepositoryByID(repoID)
	if err != nil {
		return fmt.Errorf("GetRepositoryByID: %v", err)
	}
	gitRepo, err := git.OpenRepositoryCtx(ctx, repo.RepoPath())
	if err != nil {
		return fmt.Errorf("git.OpenRepository: %v", err)
	}
	go func() {
		// FIXME: graceful: We need to tell the manager we're doing something...
		err := requests.InvalidateCodeComments(doer, gitRepo, branch)
		if err != nil {
			log.Error("PullRequestList.InvalidateCodeComments: %v", err)
		}
		gitRepo.Close()
	}()
	return nil
}

// AddTestPullRequestTask adds new test tasks by given head/base repository and head/base branch,
// and generate new patch for testing as needed.
func AddTestPullRequestTask(doer *user_model.User, repoID int64, branch string, isSync bool, oldCommitID, newCommitID string) {
	log.Trace("AddTestPullRequestTask [head_repo_id: %d, head_branch: %s]: finding pull requests", repoID, branch)
	graceful.GetManager().RunWithShutdownContext(func(ctx context.Context) {
		// There is no sensible way to shut this down ":-("
		// If you don't let it run all the way then you will lose data
		// FIXME: graceful: AddTestPullRequestTask needs to become a queue!

		prs, err := models.GetUnmergedPullRequestsByHeadInfo(repoID, branch)
		if err != nil {
			log.Error("Find pull requests [head_repo_id: %d, head_branch: %s]: %v", repoID, branch, err)
			return
		}

		if isSync {
			requests := models.PullRequestList(prs)
			if err = requests.LoadAttributes(); err != nil {
				log.Error("PullRequestList.LoadAttributes: %v", err)
			}
			if invalidationErr := checkForInvalidation(ctx, requests, repoID, doer, branch); invalidationErr != nil {
				log.Error("checkForInvalidation: %v", invalidationErr)
			}
			if err == nil {
				for _, pr := range prs {
					if newCommitID != "" && newCommitID != git.EmptySHA {
						changed, err := checkIfPRContentChanged(ctx, pr, oldCommitID, newCommitID)
						if err != nil {
							log.Error("checkIfPRContentChanged: %v", err)
						}
						if changed {
							// Mark old reviews as stale if diff to mergebase has changed
							if err := models.MarkReviewsAsStale(pr.IssueID); err != nil {
								log.Error("MarkReviewsAsStale: %v", err)
							}
						}
						if err := models.MarkReviewsAsNotStale(pr.IssueID, newCommitID); err != nil {
							log.Error("MarkReviewsAsNotStale: %v", err)
						}
						divergence, err := GetDiverging(ctx, pr)
						if err != nil {
							log.Error("GetDiverging: %v", err)
						} else {
							err = pr.UpdateCommitDivergence(divergence.Ahead, divergence.Behind)
							if err != nil {
								log.Error("UpdateCommitDivergence: %v", err)
							}
						}
					}

					pr.Issue.PullRequest = pr
					notification.NotifyPullRequestSynchronized(doer, pr)
				}
			}
		}

		for _, pr := range prs {
			log.Trace("Updating PR[%d]: composing new test task", pr.ID)
			if pr.Flow == models.PullRequestFlowGithub {
				if err := PushToBaseRepo(ctx, pr); err != nil {
					log.Error("PushToBaseRepo: %v", err)
					continue
				}
			} else {
				continue
			}

			AddToTaskQueue(pr)
			comment, err := models.CreatePushPullComment(ctx, doer, pr, oldCommitID, newCommitID)
			if err == nil && comment != nil {
				notification.NotifyPullRequestPushCommits(doer, pr, comment)
			}
		}

		log.Trace("AddTestPullRequestTask [base_repo_id: %d, base_branch: %s]: finding pull requests", repoID, branch)
		prs, err = models.GetUnmergedPullRequestsByBaseInfo(repoID, branch)
		if err != nil {
			log.Error("Find pull requests [base_repo_id: %d, base_branch: %s]: %v", repoID, branch, err)
			return
		}
		for _, pr := range prs {
			divergence, err := GetDiverging(ctx, pr)
			if err != nil {
				if models.IsErrBranchDoesNotExist(err) && !git.IsBranchExist(ctx, pr.HeadRepo.RepoPath(), pr.HeadBranch) {
					log.Warn("Cannot test PR %s/%d: head_branch %s no longer exists", pr.BaseRepo.Name, pr.IssueID, pr.HeadBranch)
				} else {
					log.Error("GetDiverging: %v", err)
				}
			} else {
				err = pr.UpdateCommitDivergence(divergence.Ahead, divergence.Behind)
				if err != nil {
					log.Error("UpdateCommitDivergence: %v", err)
				}
			}
			AddToTaskQueue(pr)
		}
	})
}

// checkIfPRContentChanged checks if diff to target branch has changed by push
// A commit can be considered to leave the PR untouched if the patch/diff with its merge base is unchanged
func checkIfPRContentChanged(ctx context.Context, pr *models.PullRequest, oldCommitID, newCommitID string) (hasChanged bool, err error) {
	if err = pr.LoadHeadRepo(); err != nil {
		return false, fmt.Errorf("LoadHeadRepo: %v", err)
	} else if pr.HeadRepo == nil {
		// corrupt data assumed changed
		return true, nil
	}

	if err = pr.LoadBaseRepo(); err != nil {
		return false, fmt.Errorf("LoadBaseRepo: %v", err)
	}

	headGitRepo, err := git.OpenRepositoryCtx(ctx, pr.HeadRepo.RepoPath())
	if err != nil {
		return false, fmt.Errorf("OpenRepository: %v", err)
	}
	defer headGitRepo.Close()

	// Add a temporary remote.
	tmpRemote := "checkIfPRContentChanged-" + fmt.Sprint(time.Now().UnixNano())
	if err = headGitRepo.AddRemote(tmpRemote, pr.BaseRepo.RepoPath(), true); err != nil {
		return false, fmt.Errorf("AddRemote: %s/%s-%s: %v", pr.HeadRepo.OwnerName, pr.HeadRepo.Name, tmpRemote, err)
	}
	defer func() {
		if err := headGitRepo.RemoveRemote(tmpRemote); err != nil {
			log.Error("checkIfPRContentChanged: RemoveRemote: %s/%s-%s: %v", pr.HeadRepo.OwnerName, pr.HeadRepo.Name, tmpRemote, err)
		}
	}()
	// To synchronize repo and get a base ref
	_, base, err := headGitRepo.GetMergeBase(tmpRemote, pr.BaseBranch, pr.HeadBranch)
	if err != nil {
		return false, fmt.Errorf("GetMergeBase: %v", err)
	}

	diffBefore := &bytes.Buffer{}
	diffAfter := &bytes.Buffer{}
	if err := headGitRepo.GetDiffFromMergeBase(base, oldCommitID, diffBefore); err != nil {
		// If old commit not found, assume changed.
		log.Debug("GetDiffFromMergeBase: %v", err)
		return true, nil
	}
	if err := headGitRepo.GetDiffFromMergeBase(base, newCommitID, diffAfter); err != nil {
		// New commit should be found
		return false, fmt.Errorf("GetDiffFromMergeBase: %v", err)
	}

	diffBeforeLines := bufio.NewScanner(diffBefore)
	diffAfterLines := bufio.NewScanner(diffAfter)

	for diffBeforeLines.Scan() && diffAfterLines.Scan() {
		if strings.HasPrefix(diffBeforeLines.Text(), "index") && strings.HasPrefix(diffAfterLines.Text(), "index") {
			// file hashes can change without the diff changing
			continue
		} else if strings.HasPrefix(diffBeforeLines.Text(), "@@") && strings.HasPrefix(diffAfterLines.Text(), "@@") {
			// the location of the difference may change
			continue
		} else if !bytes.Equal(diffBeforeLines.Bytes(), diffAfterLines.Bytes()) {
			return true, nil
		}
	}

	if diffBeforeLines.Scan() || diffAfterLines.Scan() {
		// Diffs not of equal length
		return true, nil
	}

	return false, nil
}

// PushToBaseRepo pushes commits from branches of head repository to
// corresponding branches of base repository.
// FIXME: Only push branches that are actually updates?
func PushToBaseRepo(ctx context.Context, pr *models.PullRequest) (err error) {
	return pushToBaseRepoHelper(ctx, pr, "")
}

func pushToBaseRepoHelper(ctx context.Context, pr *models.PullRequest, prefixHeadBranch string) (err error) {
	log.Trace("PushToBaseRepo[%d]: pushing commits to base repo '%s'", pr.BaseRepoID, pr.GetGitRefName())

	if err := pr.LoadHeadRepo(); err != nil {
		log.Error("Unable to load head repository for PR[%d] Error: %v", pr.ID, err)
		return err
	}
	headRepoPath := pr.HeadRepo.RepoPath()

	if err := pr.LoadBaseRepo(); err != nil {
		log.Error("Unable to load base repository for PR[%d] Error: %v", pr.ID, err)
		return err
	}
	baseRepoPath := pr.BaseRepo.RepoPath()

	if err = pr.LoadIssue(); err != nil {
		return fmt.Errorf("unable to load issue %d for pr %d: %v", pr.IssueID, pr.ID, err)
	}
	if err = pr.Issue.LoadPoster(); err != nil {
		return fmt.Errorf("unable to load poster %d for pr %d: %v", pr.Issue.PosterID, pr.ID, err)
	}

	gitRefName := pr.GetGitRefName()

	if err := git.Push(ctx, headRepoPath, git.PushOptions{
		Remote: baseRepoPath,
		Branch: prefixHeadBranch + pr.HeadBranch + ":" + gitRefName,
		Force:  true,
		// Use InternalPushingEnvironment here because we know that pre-receive and post-receive do not run on a refs/pulls/...
		Env: models.InternalPushingEnvironment(pr.Issue.Poster, pr.BaseRepo),
	}); err != nil {
		if git.IsErrPushOutOfDate(err) {
			// This should not happen as we're using force!
			log.Error("Unable to push PR head for %s#%d (%-v:%s) due to ErrPushOfDate: %v", pr.BaseRepo.FullName(), pr.Index, pr.BaseRepo, gitRefName, err)
			return err
		} else if git.IsErrPushRejected(err) {
			rejectErr := err.(*git.ErrPushRejected)
			log.Info("Unable to push PR head for %s#%d (%-v:%s) due to rejection:\nStdout: %s\nStderr: %s\nError: %v", pr.BaseRepo.FullName(), pr.Index, pr.BaseRepo, gitRefName, rejectErr.StdOut, rejectErr.StdErr, rejectErr.Err)
			return err
		} else if git.IsErrMoreThanOne(err) {
			if prefixHeadBranch != "" {
				log.Info("Can't push with %s%s", prefixHeadBranch, pr.HeadBranch)
				return err
			}
			log.Info("Retrying to push with %s%s", git.BranchPrefix, pr.HeadBranch)
			err = pushToBaseRepoHelper(ctx, pr, git.BranchPrefix)
			return err
		}
		log.Error("Unable to push PR head for %s#%d (%-v:%s) due to Error: %v", pr.BaseRepo.FullName(), pr.Index, pr.BaseRepo, gitRefName, err)
		return fmt.Errorf("Push: %s:%s %s:%s %v", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), gitRefName, err)
	}

	return nil
}

// UpdateRef update refs/pull/id/head directly for agit flow pull request
func UpdateRef(ctx context.Context, pr *models.PullRequest) (err error) {
	log.Trace("UpdateRef[%d]: upgate pull request ref in base repo '%s'", pr.ID, pr.GetGitRefName())
	if err := pr.LoadBaseRepo(); err != nil {
		log.Error("Unable to load base repository for PR[%d] Error: %v", pr.ID, err)
		return err
	}

	_, err = git.NewCommand(ctx, "update-ref", pr.GetGitRefName(), pr.HeadCommitID).RunInDir(pr.BaseRepo.RepoPath())
	if err != nil {
		log.Error("Unable to update ref in base repository for PR[%d] Error: %v", pr.ID, err)
	}

	return err
}

type errlist []error

func (errs errlist) Error() string {
	if len(errs) > 0 {
		var buf strings.Builder
		for i, err := range errs {
			if i > 0 {
				buf.WriteString(", ")
			}
			buf.WriteString(err.Error())
		}
		return buf.String()
	}
	return ""
}

// CloseBranchPulls close all the pull requests who's head branch is the branch
func CloseBranchPulls(doer *user_model.User, repoID int64, branch string) error {
	prs, err := models.GetUnmergedPullRequestsByHeadInfo(repoID, branch)
	if err != nil {
		return err
	}

	prs2, err := models.GetUnmergedPullRequestsByBaseInfo(repoID, branch)
	if err != nil {
		return err
	}

	prs = append(prs, prs2...)
	if err := models.PullRequestList(prs).LoadAttributes(); err != nil {
		return err
	}

	var errs errlist
	for _, pr := range prs {
		if err = issue_service.ChangeStatus(pr.Issue, doer, true); err != nil && !models.IsErrPullWasClosed(err) && !models.IsErrDependenciesLeft(err) {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// CloseRepoBranchesPulls close all pull requests which head branches are in the given repository, but only whose base repo is not in the given repository
func CloseRepoBranchesPulls(ctx context.Context, doer *user_model.User, repo *repo_model.Repository) error {
	branches, _, err := git.GetBranchesByPath(ctx, repo.RepoPath(), 0, 0)
	if err != nil {
		return err
	}

	var errs errlist
	for _, branch := range branches {
		prs, err := models.GetUnmergedPullRequestsByHeadInfo(repo.ID, branch.Name)
		if err != nil {
			return err
		}

		if err = models.PullRequestList(prs).LoadAttributes(); err != nil {
			return err
		}

		for _, pr := range prs {
			// If the base repository for this pr is this repository there is no need to close it
			// as it is going to be deleted anyway
			if pr.BaseRepoID == repo.ID {
				continue
			}
			if err = issue_service.ChangeStatus(pr.Issue, doer, true); err != nil && !models.IsErrPullWasClosed(err) {
				errs = append(errs, err)
			}
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

var commitMessageTrailersPattern = regexp.MustCompile(`(?:^|\n\n)(?:[\w-]+[ \t]*:[^\n]+\n*(?:[ \t]+[^\n]+\n*)*)+$`)

// GetSquashMergeCommitMessages returns the commit messages between head and merge base (if there is one)
func GetSquashMergeCommitMessages(ctx context.Context, pr *models.PullRequest) string {
	if err := pr.LoadIssue(); err != nil {
		log.Error("Cannot load issue %d for PR id %d: Error: %v", pr.IssueID, pr.ID, err)
		return ""
	}

	if err := pr.Issue.LoadPoster(); err != nil {
		log.Error("Cannot load poster %d for pr id %d, index %d Error: %v", pr.Issue.PosterID, pr.ID, pr.Index, err)
		return ""
	}

	if pr.HeadRepo == nil {
		var err error
		pr.HeadRepo, err = repo_model.GetRepositoryByID(pr.HeadRepoID)
		if err != nil {
			log.Error("GetRepositoryById[%d]: %v", pr.HeadRepoID, err)
			return ""
		}
	}

	gitRepo, closer, err := git.RepositoryFromContextOrOpen(ctx, pr.HeadRepo.RepoPath())
	if err != nil {
		log.Error("Unable to open head repository: Error: %v", err)
		return ""
	}
	defer closer.Close()

	var headCommit *git.Commit
	if pr.Flow == models.PullRequestFlowGithub {
		headCommit, err = gitRepo.GetBranchCommit(pr.HeadBranch)
	} else {
		pr.HeadCommitID, err = gitRepo.GetRefCommitID(pr.GetGitRefName())
		if err != nil {
			log.Error("Unable to get head commit: %s Error: %v", pr.GetGitRefName(), err)
			return ""
		}
		headCommit, err = gitRepo.GetCommit(pr.HeadCommitID)
	}
	if err != nil {
		log.Error("Unable to get head commit: %s Error: %v", pr.HeadBranch, err)
		return ""
	}

	mergeBase, err := gitRepo.GetCommit(pr.MergeBase)
	if err != nil {
		log.Error("Unable to get merge base commit: %s Error: %v", pr.MergeBase, err)
		return ""
	}

	limit := setting.Repository.PullRequest.DefaultMergeMessageCommitsLimit

	commits, err := gitRepo.CommitsBetweenLimit(headCommit, mergeBase, limit, 0)
	if err != nil {
		log.Error("Unable to get commits between: %s %s Error: %v", pr.HeadBranch, pr.MergeBase, err)
		return ""
	}

	posterSig := pr.Issue.Poster.NewGitSig().String()

	authorsMap := map[string]bool{}
	authors := make([]string, 0, len(commits))
	stringBuilder := strings.Builder{}

	if !setting.Repository.PullRequest.PopulateSquashCommentWithCommitMessages {
		message := strings.TrimSpace(pr.Issue.Content)
		stringBuilder.WriteString(message)
		if stringBuilder.Len() > 0 {
			stringBuilder.WriteRune('\n')
			if !commitMessageTrailersPattern.MatchString(message) {
				stringBuilder.WriteRune('\n')
			}
		}
	}

	// commits list is in reverse chronological order
	first := true
	for i := len(commits) - 1; i >= 0; i-- {
		commit := commits[i]

		if setting.Repository.PullRequest.PopulateSquashCommentWithCommitMessages {
			maxSize := setting.Repository.PullRequest.DefaultMergeMessageSize
			if maxSize < 0 || stringBuilder.Len() < maxSize {
				var toWrite []byte
				if first {
					first = false
					toWrite = []byte(strings.TrimPrefix(commit.CommitMessage, pr.Issue.Title))
				} else {
					toWrite = []byte(commit.CommitMessage)
				}

				if len(toWrite) > maxSize-stringBuilder.Len() && maxSize > -1 {
					toWrite = append(toWrite[:maxSize-stringBuilder.Len()], "..."...)
				}
				if _, err := stringBuilder.Write(toWrite); err != nil {
					log.Error("Unable to write commit message Error: %v", err)
					return ""
				}

				if _, err := stringBuilder.WriteRune('\n'); err != nil {
					log.Error("Unable to write commit message Error: %v", err)
					return ""
				}
			}
		}

		authorString := commit.Author.String()
		if !authorsMap[authorString] && authorString != posterSig {
			authors = append(authors, authorString)
			authorsMap[authorString] = true
		}
	}

	// Consider collecting the remaining authors
	if limit >= 0 && setting.Repository.PullRequest.DefaultMergeMessageAllAuthors {
		skip := limit
		limit = 30
		for {
			commits, err := gitRepo.CommitsBetweenLimit(headCommit, mergeBase, limit, skip)
			if err != nil {
				log.Error("Unable to get commits between: %s %s Error: %v", pr.HeadBranch, pr.MergeBase, err)
				return ""

			}
			if len(commits) == 0 {
				break
			}
			for _, commit := range commits {
				authorString := commit.Author.String()
				if !authorsMap[authorString] && authorString != posterSig {
					authors = append(authors, authorString)
					authorsMap[authorString] = true
				}
			}
			skip += limit
		}
	}

	for _, author := range authors {
		if _, err := stringBuilder.Write([]byte("Co-authored-by: ")); err != nil {
			log.Error("Unable to write to string builder Error: %v", err)
			return ""
		}
		if _, err := stringBuilder.Write([]byte(author)); err != nil {
			log.Error("Unable to write to string builder Error: %v", err)
			return ""
		}
		if _, err := stringBuilder.WriteRune('\n'); err != nil {
			log.Error("Unable to write to string builder Error: %v", err)
			return ""
		}
	}

	return stringBuilder.String()
}

// GetIssuesLastCommitStatus returns a map
func GetIssuesLastCommitStatus(ctx context.Context, issues models.IssueList) (map[int64]*models.CommitStatus, error) {
	if err := issues.LoadPullRequests(); err != nil {
		return nil, err
	}
	if _, err := issues.LoadRepositories(); err != nil {
		return nil, err
	}

	var (
		gitRepos = make(map[int64]*git.Repository)
		res      = make(map[int64]*models.CommitStatus)
		err      error
	)
	defer func() {
		for _, gitRepo := range gitRepos {
			gitRepo.Close()
		}
	}()

	for _, issue := range issues {
		if !issue.IsPull {
			continue
		}
		gitRepo, ok := gitRepos[issue.RepoID]
		if !ok {
			gitRepo, err = git.OpenRepositoryCtx(ctx, issue.Repo.RepoPath())
			if err != nil {
				log.Error("Cannot open git repository %-v for issue #%d[%d]. Error: %v", issue.Repo, issue.Index, issue.ID, err)
				continue
			}
			gitRepos[issue.RepoID] = gitRepo
		}

		status, err := getLastCommitStatus(gitRepo, issue.PullRequest)
		if err != nil {
			log.Error("getLastCommitStatus: cant get last commit of pull [%d]: %v", issue.PullRequest.ID, err)
			continue
		}
		res[issue.PullRequest.ID] = status
	}
	return res, nil
}

// getLastCommitStatus get pr's last commit status. PR's last commit status is the head commit id's last commit status
func getLastCommitStatus(gitRepo *git.Repository, pr *models.PullRequest) (status *models.CommitStatus, err error) {
	sha, err := gitRepo.GetRefCommitID(pr.GetGitRefName())
	if err != nil {
		return nil, err
	}

	statusList, _, err := models.GetLatestCommitStatus(pr.BaseRepo.ID, sha, db.ListOptions{})
	if err != nil {
		return nil, err
	}
	return models.CalcCommitStatus(statusList), nil
}

// IsHeadEqualWithBranch returns if the commits of branchName are available in pull request head
func IsHeadEqualWithBranch(ctx context.Context, pr *models.PullRequest, branchName string) (bool, error) {
	var err error
	if err = pr.LoadBaseRepo(); err != nil {
		return false, err
	}
	baseGitRepo, closer, err := git.RepositoryFromContextOrOpen(ctx, pr.BaseRepo.RepoPath())
	if err != nil {
		return false, err
	}
	defer closer.Close()

	baseCommit, err := baseGitRepo.GetBranchCommit(branchName)
	if err != nil {
		return false, err
	}

	if err = pr.LoadHeadRepo(); err != nil {
		return false, err
	}
	var headGitRepo *git.Repository
	if pr.HeadRepoID == pr.BaseRepoID {
		headGitRepo = baseGitRepo
	} else {
		var closer io.Closer

		headGitRepo, closer, err = git.RepositoryFromContextOrOpen(ctx, pr.HeadRepo.RepoPath())
		if err != nil {
			return false, err
		}
		defer closer.Close()
	}

	var headCommit *git.Commit
	if pr.Flow == models.PullRequestFlowGithub {
		headCommit, err = headGitRepo.GetBranchCommit(pr.HeadBranch)
		if err != nil {
			return false, err
		}
	} else {
		pr.HeadCommitID, err = baseGitRepo.GetRefCommitID(pr.GetGitRefName())
		if err != nil {
			return false, err
		}
		if headCommit, err = baseGitRepo.GetCommit(pr.HeadCommitID); err != nil {
			return false, err
		}
	}
	return baseCommit.HasPreviousCommit(headCommit.ID)
}
