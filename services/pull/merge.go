// Copyright 2019 The Gitea Authors.
// All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package pull

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"code.gitea.io/gitea/models"
	"code.gitea.io/gitea/models/db"
	repo_model "code.gitea.io/gitea/models/repo"
	"code.gitea.io/gitea/models/unit"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/cache"
	"code.gitea.io/gitea/modules/git"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/notification"
	"code.gitea.io/gitea/modules/references"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/modules/timeutil"
	asymkey_service "code.gitea.io/gitea/services/asymkey"
	issue_service "code.gitea.io/gitea/services/issue"
)

// Merge merges pull request to base repository.
// Caller should check PR is ready to be merged (review and status checks)
// FIXME: add repoWorkingPull make sure two merges does not happen at same time.
func Merge(ctx context.Context, pr *models.PullRequest, doer *user_model.User, baseGitRepo *git.Repository, mergeStyle repo_model.MergeStyle, expectedHeadCommitID, message string) (err error) {
	if err = pr.LoadHeadRepo(); err != nil {
		log.Error("LoadHeadRepo: %v", err)
		return fmt.Errorf("LoadHeadRepo: %v", err)
	} else if err = pr.LoadBaseRepo(); err != nil {
		log.Error("LoadBaseRepo: %v", err)
		return fmt.Errorf("LoadBaseRepo: %v", err)
	}

	prUnit, err := pr.BaseRepo.GetUnit(unit.TypePullRequests)
	if err != nil {
		log.Error("pr.BaseRepo.GetUnit(unit.TypePullRequests): %v", err)
		return err
	}
	prConfig := prUnit.PullRequestsConfig()

	// Check if merge style is correct and allowed
	if !prConfig.IsMergeStyleAllowed(mergeStyle) {
		return models.ErrInvalidMergeStyle{ID: pr.BaseRepo.ID, Style: mergeStyle}
	}

	defer func() {
		go AddTestPullRequestTask(doer, pr.BaseRepo.ID, pr.BaseBranch, false, "", "")
	}()

	pr.MergedCommitID, err = rawMerge(ctx, pr, doer, mergeStyle, expectedHeadCommitID, message)
	if err != nil {
		return err
	}

	pr.MergedUnix = timeutil.TimeStampNow()
	pr.Merger = doer
	pr.MergerID = doer.ID

	if _, err := pr.SetMerged(); err != nil {
		log.Error("setMerged [%d]: %v", pr.ID, err)
	}

	if err := pr.LoadIssue(); err != nil {
		log.Error("loadIssue [%d]: %v", pr.ID, err)
	}

	if err := pr.Issue.LoadRepo(); err != nil {
		log.Error("loadRepo for issue [%d]: %v", pr.ID, err)
	}
	if err := pr.Issue.Repo.GetOwner(db.DefaultContext); err != nil {
		log.Error("GetOwner for issue repo [%d]: %v", pr.ID, err)
	}

	notification.NotifyMergePullRequest(pr, doer)

	// Reset cached commit count
	cache.Remove(pr.Issue.Repo.GetCommitsCountCacheKey(pr.BaseBranch, true))

	// Resolve cross references
	refs, err := pr.ResolveCrossReferences()
	if err != nil {
		log.Error("ResolveCrossReferences: %v", err)
		return nil
	}

	for _, ref := range refs {
		if err = ref.LoadIssue(); err != nil {
			return err
		}
		if err = ref.Issue.LoadRepo(); err != nil {
			return err
		}
		close := ref.RefAction == references.XRefActionCloses
		if close != ref.Issue.IsClosed {
			if err = issue_service.ChangeStatus(ref.Issue, doer, close); err != nil {
				// Allow ErrDependenciesLeft
				if !models.IsErrDependenciesLeft(err) {
					return err
				}
			}
		}
	}

	return nil
}

// rawMerge perform the merge operation without changing any pull information in database
func rawMerge(ctx context.Context, pr *models.PullRequest, doer *user_model.User, mergeStyle repo_model.MergeStyle, expectedHeadCommitID, message string) (string, error) {
	err := git.LoadGitVersion()
	if err != nil {
		log.Error("git.LoadGitVersion: %v", err)
		return "", fmt.Errorf("Unable to get git version: %v", err)
	}

	// Clone base repo.
	tmpBasePath, err := createTemporaryRepo(ctx, pr)
	if err != nil {
		log.Error("CreateTemporaryPath: %v", err)
		return "", err
	}
	defer func() {
		if err := models.RemoveTemporaryPath(tmpBasePath); err != nil {
			log.Error("Merge: RemoveTemporaryPath: %s", err)
		}
	}()

	baseBranch := "base"
	trackingBranch := "tracking"
	stagingBranch := "staging"

	if expectedHeadCommitID != "" {
		trackingCommitID, err := git.NewCommand(ctx, "show-ref", "--hash", git.BranchPrefix+trackingBranch).RunInDir(tmpBasePath)
		if err != nil {
			log.Error("show-ref[%s] --hash refs/heads/trackingn: %v", tmpBasePath, git.BranchPrefix+trackingBranch, err)
			return "", fmt.Errorf("getDiffTree: %v", err)
		}
		if strings.TrimSpace(trackingCommitID) != expectedHeadCommitID {
			return "", models.ErrSHADoesNotMatch{
				GivenSHA:   expectedHeadCommitID,
				CurrentSHA: trackingCommitID,
			}
		}
	}

	var outbuf, errbuf strings.Builder

	// Enable sparse-checkout
	sparseCheckoutList, err := getDiffTree(ctx, tmpBasePath, baseBranch, trackingBranch)
	if err != nil {
		log.Error("getDiffTree(%s, %s, %s): %v", tmpBasePath, baseBranch, trackingBranch, err)
		return "", fmt.Errorf("getDiffTree: %v", err)
	}

	infoPath := filepath.Join(tmpBasePath, ".git", "info")
	if err := os.MkdirAll(infoPath, 0o700); err != nil {
		log.Error("Unable to create .git/info in %s: %v", tmpBasePath, err)
		return "", fmt.Errorf("Unable to create .git/info in tmpBasePath: %v", err)
	}

	sparseCheckoutListPath := filepath.Join(infoPath, "sparse-checkout")
	if err := os.WriteFile(sparseCheckoutListPath, []byte(sparseCheckoutList), 0o600); err != nil {
		log.Error("Unable to write .git/info/sparse-checkout file in %s: %v", tmpBasePath, err)
		return "", fmt.Errorf("Unable to write .git/info/sparse-checkout file in tmpBasePath: %v", err)
	}

	var gitConfigCommand func() *git.Command
	if git.CheckGitVersionAtLeast("1.8.0") == nil {
		gitConfigCommand = func() *git.Command {
			return git.NewCommand(ctx, "config", "--local")
		}
	} else {
		gitConfigCommand = func() *git.Command {
			return git.NewCommand(ctx, "config")
		}
	}

	// Switch off LFS process (set required, clean and smudge here also)
	if err := gitConfigCommand().AddArguments("filter.lfs.process", "").
		RunWithContext(&git.RunContext{
			Timeout: -1,
			Dir:     tmpBasePath,
			Stdout:  &outbuf,
			Stderr:  &errbuf,
		}); err != nil {
		log.Error("git config [filter.lfs.process -> <> ]: %v\n%s\n%s", err, outbuf.String(), errbuf.String())
		return "", fmt.Errorf("git config [filter.lfs.process -> <> ]: %v\n%s\n%s", err, outbuf.String(), errbuf.String())
	}
	outbuf.Reset()
	errbuf.Reset()

	if err := gitConfigCommand().AddArguments("filter.lfs.required", "false").
		RunWithContext(&git.RunContext{
			Timeout: -1,
			Dir:     tmpBasePath,
			Stdout:  &outbuf,
			Stderr:  &errbuf,
		}); err != nil {
		log.Error("git config [filter.lfs.required -> <false> ]: %v\n%s\n%s", err, outbuf.String(), errbuf.String())
		return "", fmt.Errorf("git config [filter.lfs.required -> <false> ]: %v\n%s\n%s", err, outbuf.String(), errbuf.String())
	}
	outbuf.Reset()
	errbuf.Reset()

	if err := gitConfigCommand().AddArguments("filter.lfs.clean", "").
		RunWithContext(&git.RunContext{
			Timeout: -1,
			Dir:     tmpBasePath,
			Stdout:  &outbuf,
			Stderr:  &errbuf,
		}); err != nil {
		log.Error("git config [filter.lfs.clean -> <> ]: %v\n%s\n%s", err, outbuf.String(), errbuf.String())
		return "", fmt.Errorf("git config [filter.lfs.clean -> <> ]: %v\n%s\n%s", err, outbuf.String(), errbuf.String())
	}
	outbuf.Reset()
	errbuf.Reset()

	if err := gitConfigCommand().AddArguments("filter.lfs.smudge", "").
		RunWithContext(&git.RunContext{
			Timeout: -1,
			Dir:     tmpBasePath,
			Stdout:  &outbuf,
			Stderr:  &errbuf,
		}); err != nil {
		log.Error("git config [filter.lfs.smudge -> <> ]: %v\n%s\n%s", err, outbuf.String(), errbuf.String())
		return "", fmt.Errorf("git config [filter.lfs.smudge -> <> ]: %v\n%s\n%s", err, outbuf.String(), errbuf.String())
	}
	outbuf.Reset()
	errbuf.Reset()

	if err := gitConfigCommand().AddArguments("core.sparseCheckout", "true").
		RunWithContext(&git.RunContext{
			Timeout: -1,
			Dir:     tmpBasePath,
			Stdout:  &outbuf,
			Stderr:  &errbuf,
		}); err != nil {
		log.Error("git config [core.sparseCheckout -> true ]: %v\n%s\n%s", err, outbuf.String(), errbuf.String())
		return "", fmt.Errorf("git config [core.sparsecheckout -> true]: %v\n%s\n%s", err, outbuf.String(), errbuf.String())
	}
	outbuf.Reset()
	errbuf.Reset()

	// Read base branch index
	if err := git.NewCommand(ctx, "read-tree", "HEAD").
		RunWithContext(&git.RunContext{
			Timeout: -1,
			Dir:     tmpBasePath,
			Stdout:  &outbuf,
			Stderr:  &errbuf,
		}); err != nil {
		log.Error("git read-tree HEAD: %v\n%s\n%s", err, outbuf.String(), errbuf.String())
		return "", fmt.Errorf("Unable to read base branch in to the index: %v\n%s\n%s", err, outbuf.String(), errbuf.String())
	}
	outbuf.Reset()
	errbuf.Reset()

	sig := doer.NewGitSig()
	committer := sig

	// Determine if we should sign
	signArg := ""
	if git.CheckGitVersionAtLeast("1.7.9") == nil {
		sign, keyID, signer, _ := asymkey_service.SignMerge(ctx, pr, doer, tmpBasePath, "HEAD", trackingBranch)
		if sign {
			signArg = "-S" + keyID
			if pr.BaseRepo.GetTrustModel() == repo_model.CommitterTrustModel || pr.BaseRepo.GetTrustModel() == repo_model.CollaboratorCommitterTrustModel {
				committer = signer
			}
		} else if git.CheckGitVersionAtLeast("2.0.0") == nil {
			signArg = "--no-gpg-sign"
		}
	}

	commitTimeStr := time.Now().Format(time.RFC3339)

	// Because this may call hooks we should pass in the environment
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME="+sig.Name,
		"GIT_AUTHOR_EMAIL="+sig.Email,
		"GIT_AUTHOR_DATE="+commitTimeStr,
		"GIT_COMMITTER_NAME="+committer.Name,
		"GIT_COMMITTER_EMAIL="+committer.Email,
		"GIT_COMMITTER_DATE="+commitTimeStr,
	)

	// Merge commits.
	switch mergeStyle {
	case repo_model.MergeStyleMerge:
		cmd := git.NewCommand(ctx, "merge", "--no-ff", "--no-commit", trackingBranch)
		if err := runMergeCommand(pr, mergeStyle, cmd, tmpBasePath); err != nil {
			log.Error("Unable to merge tracking into base: %v", err)
			return "", err
		}

		if err := commitAndSignNoAuthor(ctx, pr, message, signArg, tmpBasePath, env); err != nil {
			log.Error("Unable to make final commit: %v", err)
			return "", err
		}
	case repo_model.MergeStyleRebase:
		fallthrough
	case repo_model.MergeStyleRebaseUpdate:
		fallthrough
	case repo_model.MergeStyleRebaseMerge:
		// Checkout head branch
		if err := git.NewCommand(ctx, "checkout", "-b", stagingBranch, trackingBranch).
			RunWithContext(&git.RunContext{
				Timeout: -1,
				Dir:     tmpBasePath,
				Stdout:  &outbuf,
				Stderr:  &errbuf,
			}); err != nil {
			log.Error("git checkout base prior to merge post staging rebase [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
			return "", fmt.Errorf("git checkout base prior to merge post staging rebase  [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
		}
		outbuf.Reset()
		errbuf.Reset()

		// Rebase before merging
		if err := git.NewCommand(ctx, "rebase", baseBranch).
			RunWithContext(&git.RunContext{
				Timeout: -1,
				Dir:     tmpBasePath,
				Stdout:  &outbuf,
				Stderr:  &errbuf,
			}); err != nil {
			// Rebase will leave a REBASE_HEAD file in .git if there is a conflict
			if _, statErr := os.Stat(filepath.Join(tmpBasePath, ".git", "REBASE_HEAD")); statErr == nil {
				var commitSha string
				ok := false
				failingCommitPaths := []string{
					filepath.Join(tmpBasePath, ".git", "rebase-apply", "original-commit"), // Git < 2.26
					filepath.Join(tmpBasePath, ".git", "rebase-merge", "stopped-sha"),     // Git >= 2.26
				}
				for _, failingCommitPath := range failingCommitPaths {
					if _, statErr := os.Stat(failingCommitPath); statErr == nil {
						commitShaBytes, readErr := os.ReadFile(failingCommitPath)
						if readErr != nil {
							// Abandon this attempt to handle the error
							log.Error("git rebase staging on to base [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
							return "", fmt.Errorf("git rebase staging on to base [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
						}
						commitSha = strings.TrimSpace(string(commitShaBytes))
						ok = true
						break
					}
				}
				if !ok {
					log.Error("Unable to determine failing commit sha for this rebase message. Cannot cast as models.ErrRebaseConflicts.")
					log.Error("git rebase staging on to base [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
					return "", fmt.Errorf("git rebase staging on to base [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
				}
				log.Debug("RebaseConflict at %s [%s:%s -> %s:%s]: %v\n%s\n%s", commitSha, pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
				return "", models.ErrRebaseConflicts{
					Style:     mergeStyle,
					CommitSHA: commitSha,
					StdOut:    outbuf.String(),
					StdErr:    errbuf.String(),
					Err:       err,
				}
			}
			log.Error("git rebase staging on to base [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
			return "", fmt.Errorf("git rebase staging on to base [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
		}
		outbuf.Reset()
		errbuf.Reset()

		// not need merge, just update by rebase. so skip
		if mergeStyle == repo_model.MergeStyleRebaseUpdate {
			break
		}

		// Checkout base branch again
		if err := git.NewCommand(ctx, "checkout", baseBranch).
			RunWithContext(&git.RunContext{
				Timeout: -1,
				Dir:     tmpBasePath,
				Stdout:  &outbuf,
				Stderr:  &errbuf,
			}); err != nil {
			log.Error("git checkout base prior to merge post staging rebase [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
			return "", fmt.Errorf("git checkout base prior to merge post staging rebase  [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
		}
		outbuf.Reset()
		errbuf.Reset()

		cmd := git.NewCommand(ctx, "merge")
		if mergeStyle == repo_model.MergeStyleRebase {
			cmd.AddArguments("--ff-only")
		} else {
			cmd.AddArguments("--no-ff", "--no-commit")
		}
		cmd.AddArguments(stagingBranch)

		// Prepare merge with commit
		if err := runMergeCommand(pr, mergeStyle, cmd, tmpBasePath); err != nil {
			log.Error("Unable to merge staging into base: %v", err)
			return "", err
		}
		if mergeStyle == repo_model.MergeStyleRebaseMerge {
			if err := commitAndSignNoAuthor(ctx, pr, message, signArg, tmpBasePath, env); err != nil {
				log.Error("Unable to make final commit: %v", err)
				return "", err
			}
		}
	case repo_model.MergeStyleSquash:
		// Merge with squash
		cmd := git.NewCommand(ctx, "merge", "--squash", trackingBranch)
		if err := runMergeCommand(pr, mergeStyle, cmd, tmpBasePath); err != nil {
			log.Error("Unable to merge --squash tracking into base: %v", err)
			return "", err
		}

		if err = pr.Issue.LoadPoster(); err != nil {
			log.Error("LoadPoster: %v", err)
			return "", fmt.Errorf("LoadPoster: %v", err)
		}
		sig := pr.Issue.Poster.NewGitSig()
		if signArg == "" {
			if err := git.NewCommand(ctx, "commit", fmt.Sprintf("--author='%s <%s>'", sig.Name, sig.Email), "-m", message).
				RunWithContext(&git.RunContext{
					Env:     env,
					Timeout: -1,
					Dir:     tmpBasePath,
					Stdout:  &outbuf,
					Stderr:  &errbuf,
				}); err != nil {
				log.Error("git commit [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
				return "", fmt.Errorf("git commit [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
			}
		} else {
			if setting.Repository.PullRequest.AddCoCommitterTrailers && committer.String() != sig.String() {
				// add trailer
				message += fmt.Sprintf("\nCo-authored-by: %s\nCo-committed-by: %s\n", sig.String(), sig.String())
			}
			if err := git.NewCommand(ctx, "commit", signArg, fmt.Sprintf("--author='%s <%s>'", sig.Name, sig.Email), "-m", message).
				RunWithContext(&git.RunContext{
					Env:     env,
					Timeout: -1,
					Dir:     tmpBasePath,
					Stdout:  &outbuf,
					Stderr:  &errbuf,
				}); err != nil {
				log.Error("git commit [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
				return "", fmt.Errorf("git commit [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
			}
		}
		outbuf.Reset()
		errbuf.Reset()
	default:
		return "", models.ErrInvalidMergeStyle{ID: pr.BaseRepo.ID, Style: mergeStyle}
	}

	// OK we should cache our current head and origin/headbranch
	mergeHeadSHA, err := git.GetFullCommitID(ctx, tmpBasePath, "HEAD")
	if err != nil {
		return "", fmt.Errorf("Failed to get full commit id for HEAD: %v", err)
	}
	mergeBaseSHA, err := git.GetFullCommitID(ctx, tmpBasePath, "original_"+baseBranch)
	if err != nil {
		return "", fmt.Errorf("Failed to get full commit id for origin/%s: %v", pr.BaseBranch, err)
	}
	mergeCommitID, err := git.GetFullCommitID(ctx, tmpBasePath, baseBranch)
	if err != nil {
		return "", fmt.Errorf("Failed to get full commit id for the new merge: %v", err)
	}

	// Now it's questionable about where this should go - either after or before the push
	// I think in the interests of data safety - failures to push to the lfs should prevent
	// the merge as you can always remerge.
	if setting.LFS.StartServer {
		if err := LFSPush(ctx, tmpBasePath, mergeHeadSHA, mergeBaseSHA, pr); err != nil {
			return "", err
		}
	}

	var headUser *user_model.User
	err = pr.HeadRepo.GetOwner(db.DefaultContext)
	if err != nil {
		if !user_model.IsErrUserNotExist(err) {
			log.Error("Can't find user: %d for head repository - %v", pr.HeadRepo.OwnerID, err)
			return "", err
		}
		log.Error("Can't find user: %d for head repository - defaulting to doer: %s - %v", pr.HeadRepo.OwnerID, doer.Name, err)
		headUser = doer
	} else {
		headUser = pr.HeadRepo.Owner
	}

	env = models.FullPushingEnvironment(
		headUser,
		doer,
		pr.BaseRepo,
		pr.BaseRepo.Name,
		pr.ID,
	)

	var pushCmd *git.Command
	if mergeStyle == repo_model.MergeStyleRebaseUpdate {
		// force push the rebase result to head branch
		pushCmd = git.NewCommand(ctx, "push", "-f", "head_repo", stagingBranch+":"+git.BranchPrefix+pr.HeadBranch)
	} else {
		pushCmd = git.NewCommand(ctx, "push", "origin", baseBranch+":"+git.BranchPrefix+pr.BaseBranch)
	}

	// Push back to upstream.
	if err := pushCmd.RunWithContext(&git.RunContext{
		Env:     env,
		Timeout: -1,
		Dir:     tmpBasePath,
		Stdout:  &outbuf,
		Stderr:  &errbuf,
	}); err != nil {
		if strings.Contains(errbuf.String(), "non-fast-forward") {
			return "", &git.ErrPushOutOfDate{
				StdOut: outbuf.String(),
				StdErr: errbuf.String(),
				Err:    err,
			}
		} else if strings.Contains(errbuf.String(), "! [remote rejected]") {
			err := &git.ErrPushRejected{
				StdOut: outbuf.String(),
				StdErr: errbuf.String(),
				Err:    err,
			}
			err.GenerateMessage()
			return "", err
		}
		return "", fmt.Errorf("git push: %s", errbuf.String())
	}
	outbuf.Reset()
	errbuf.Reset()

	return mergeCommitID, nil
}

func commitAndSignNoAuthor(ctx context.Context, pr *models.PullRequest, message, signArg, tmpBasePath string, env []string) error {
	var outbuf, errbuf strings.Builder
	if signArg == "" {
		if err := git.NewCommand(ctx, "commit", "-m", message).
			RunWithContext(&git.RunContext{
				Env:     env,
				Timeout: -1,
				Dir:     tmpBasePath,
				Stdout:  &outbuf,
				Stderr:  &errbuf,
			}); err != nil {
			log.Error("git commit [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
			return fmt.Errorf("git commit [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
		}
	} else {
		if err := git.NewCommand(ctx, "commit", signArg, "-m", message).
			RunWithContext(&git.RunContext{
				Env:     env,
				Timeout: -1,
				Dir:     tmpBasePath,
				Stdout:  &outbuf,
				Stderr:  &errbuf,
			}); err != nil {
			log.Error("git commit [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
			return fmt.Errorf("git commit [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
		}
	}
	return nil
}

func runMergeCommand(pr *models.PullRequest, mergeStyle repo_model.MergeStyle, cmd *git.Command, tmpBasePath string) error {
	var outbuf, errbuf strings.Builder
	if err := cmd.RunWithContext(&git.RunContext{
		Timeout: -1,
		Dir:     tmpBasePath,
		Stdout:  &outbuf,
		Stderr:  &errbuf,
	}); err != nil {
		// Merge will leave a MERGE_HEAD file in the .git folder if there is a conflict
		if _, statErr := os.Stat(filepath.Join(tmpBasePath, ".git", "MERGE_HEAD")); statErr == nil {
			// We have a merge conflict error
			log.Debug("MergeConflict [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
			return models.ErrMergeConflicts{
				Style:  mergeStyle,
				StdOut: outbuf.String(),
				StdErr: errbuf.String(),
				Err:    err,
			}
		} else if strings.Contains(errbuf.String(), "refusing to merge unrelated histories") {
			log.Debug("MergeUnrelatedHistories [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
			return models.ErrMergeUnrelatedHistories{
				Style:  mergeStyle,
				StdOut: outbuf.String(),
				StdErr: errbuf.String(),
				Err:    err,
			}
		}
		log.Error("git merge [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
		return fmt.Errorf("git merge [%s:%s -> %s:%s]: %v\n%s\n%s", pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseRepo.FullName(), pr.BaseBranch, err, outbuf.String(), errbuf.String())
	}

	return nil
}

var escapedSymbols = regexp.MustCompile(`([*[?! \\])`)

func getDiffTree(ctx context.Context, repoPath, baseBranch, headBranch string) (string, error) {
	getDiffTreeFromBranch := func(repoPath, baseBranch, headBranch string) (string, error) {
		var outbuf, errbuf strings.Builder
		// Compute the diff-tree for sparse-checkout
		if err := git.NewCommand(ctx, "diff-tree", "--no-commit-id", "--name-only", "-r", "-z", "--root", baseBranch, headBranch, "--").
			RunWithContext(&git.RunContext{
				Timeout: -1,
				Dir:     repoPath,
				Stdout:  &outbuf,
				Stderr:  &errbuf,
			}); err != nil {
			return "", fmt.Errorf("git diff-tree [%s base:%s head:%s]: %s", repoPath, baseBranch, headBranch, errbuf.String())
		}
		return outbuf.String(), nil
	}

	scanNullTerminatedStrings := func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := bytes.IndexByte(data, '\x00'); i >= 0 {
			return i + 1, data[0:i], nil
		}
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil
	}

	list, err := getDiffTreeFromBranch(repoPath, baseBranch, headBranch)
	if err != nil {
		return "", err
	}

	// Prefixing '/' for each entry, otherwise all files with the same name in subdirectories would be matched.
	out := bytes.Buffer{}
	scanner := bufio.NewScanner(strings.NewReader(list))
	scanner.Split(scanNullTerminatedStrings)
	for scanner.Scan() {
		filepath := scanner.Text()
		// escape '*', '?', '[', spaces and '!' prefix
		filepath = escapedSymbols.ReplaceAllString(filepath, `\$1`)
		// no necessary to escape the first '#' symbol because the first symbol is '/'
		fmt.Fprintf(&out, "/%s\n", filepath)
	}

	return out.String(), nil
}

// IsSignedIfRequired check if merge will be signed if required
func IsSignedIfRequired(ctx context.Context, pr *models.PullRequest, doer *user_model.User) (bool, error) {
	if err := pr.LoadProtectedBranch(); err != nil {
		return false, err
	}

	if pr.ProtectedBranch == nil || !pr.ProtectedBranch.RequireSignedCommits {
		return true, nil
	}

	sign, _, _, err := asymkey_service.SignMerge(ctx, pr, doer, pr.BaseRepo.RepoPath(), pr.BaseBranch, pr.GetGitRefName())

	return sign, err
}

// IsUserAllowedToMerge check if user is allowed to merge PR with given permissions and branch protections
func IsUserAllowedToMerge(pr *models.PullRequest, p models.Permission, user *user_model.User) (bool, error) {
	if user == nil {
		return false, nil
	}

	err := pr.LoadProtectedBranch()
	if err != nil {
		return false, err
	}

	if (p.CanWrite(unit.TypeCode) && pr.ProtectedBranch == nil) || (pr.ProtectedBranch != nil && models.IsUserMergeWhitelisted(pr.ProtectedBranch, user.ID, p)) {
		return true, nil
	}

	return false, nil
}

// CheckPRReadyToMerge checks whether the PR is ready to be merged (reviews and status checks)
func CheckPRReadyToMerge(ctx context.Context, pr *models.PullRequest, skipProtectedFilesCheck bool) (err error) {
	if err = pr.LoadBaseRepo(); err != nil {
		return fmt.Errorf("LoadBaseRepo: %v", err)
	}

	if err = pr.LoadProtectedBranch(); err != nil {
		return fmt.Errorf("LoadProtectedBranch: %v", err)
	}
	if pr.ProtectedBranch == nil {
		return nil
	}

	isPass, err := IsPullCommitStatusPass(ctx, pr)
	if err != nil {
		return err
	}
	if !isPass {
		return models.ErrNotAllowedToMerge{
			Reason: "Not all required status checks successful",
		}
	}

	if !pr.ProtectedBranch.HasEnoughApprovals(pr) {
		return models.ErrNotAllowedToMerge{
			Reason: "Does not have enough approvals",
		}
	}
	if pr.ProtectedBranch.MergeBlockedByRejectedReview(pr) {
		return models.ErrNotAllowedToMerge{
			Reason: "There are requested changes",
		}
	}
	if pr.ProtectedBranch.MergeBlockedByOfficialReviewRequests(pr) {
		return models.ErrNotAllowedToMerge{
			Reason: "There are official review requests",
		}
	}

	if pr.ProtectedBranch.MergeBlockedByOutdatedBranch(pr) {
		return models.ErrNotAllowedToMerge{
			Reason: "The head branch is behind the base branch",
		}
	}

	if skipProtectedFilesCheck {
		return nil
	}

	if pr.ProtectedBranch.MergeBlockedByProtectedFiles(pr) {
		return models.ErrNotAllowedToMerge{
			Reason: "Changed protected files",
		}
	}

	return nil
}

// MergedManually mark pr as merged manually
func MergedManually(pr *models.PullRequest, doer *user_model.User, baseGitRepo *git.Repository, commitID string) (err error) {
	prUnit, err := pr.BaseRepo.GetUnit(unit.TypePullRequests)
	if err != nil {
		return
	}
	prConfig := prUnit.PullRequestsConfig()

	// Check if merge style is correct and allowed
	if !prConfig.IsMergeStyleAllowed(repo_model.MergeStyleManuallyMerged) {
		return models.ErrInvalidMergeStyle{ID: pr.BaseRepo.ID, Style: repo_model.MergeStyleManuallyMerged}
	}

	if len(commitID) < 40 {
		return fmt.Errorf("Wrong commit ID")
	}

	commit, err := baseGitRepo.GetCommit(commitID)
	if err != nil {
		if git.IsErrNotExist(err) {
			return fmt.Errorf("Wrong commit ID")
		}
		return
	}

	ok, err := baseGitRepo.IsCommitInBranch(commitID, pr.BaseBranch)
	if err != nil {
		return
	}
	if !ok {
		return fmt.Errorf("Wrong commit ID")
	}

	pr.MergedCommitID = commitID
	pr.MergedUnix = timeutil.TimeStamp(commit.Author.When.Unix())
	pr.Status = models.PullRequestStatusManuallyMerged
	pr.Merger = doer
	pr.MergerID = doer.ID

	merged := false
	if merged, err = pr.SetMerged(); err != nil {
		return
	} else if !merged {
		return fmt.Errorf("SetMerged failed")
	}

	notification.NotifyMergePullRequest(pr, doer)
	log.Info("manuallyMerged[%d]: Marked as manually merged into %s/%s by commit id: %s", pr.ID, pr.BaseRepo.Name, pr.BaseBranch, commit.ID.String())
	return nil
}
