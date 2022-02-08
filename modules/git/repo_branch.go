// Copyright 2015 The Gogs Authors. All rights reserved.
// Copyright 2018 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package git

import (
	"context"
	"fmt"
	"strings"
)

// BranchPrefix base dir of the branch information file store on git
const BranchPrefix = "refs/heads/"

// AGit Flow

// PullRequestPrefix special ref to create a pull request: refs/for/<targe-branch>/<topic-branch>
// or refs/for/<targe-branch> -o topic='<topic-branch>'
const PullRequestPrefix = "refs/for/"

// TODO: /refs/for-review for suggest change interface

// IsReferenceExist returns true if given reference exists in the repository.
func IsReferenceExist(ctx context.Context, repoPath, name string) bool {
	_, err := NewCommand(ctx, "show-ref", "--verify", "--", name).RunInDir(repoPath)
	return err == nil
}

// IsBranchExist returns true if given branch exists in the repository.
func IsBranchExist(ctx context.Context, repoPath, name string) bool {
	return IsReferenceExist(ctx, repoPath, BranchPrefix+name)
}

// Branch represents a Git branch.
type Branch struct {
	Name string
	Path string

	gitRepo *Repository
}

// GetHEADBranch returns corresponding branch of HEAD.
func (repo *Repository) GetHEADBranch() (*Branch, error) {
	if repo == nil {
		return nil, fmt.Errorf("nil repo")
	}
	stdout, err := NewCommand(repo.Ctx, "symbolic-ref", "HEAD").RunInDir(repo.Path)
	if err != nil {
		return nil, err
	}
	stdout = strings.TrimSpace(stdout)

	if !strings.HasPrefix(stdout, BranchPrefix) {
		return nil, fmt.Errorf("invalid HEAD branch: %v", stdout)
	}

	return &Branch{
		Name:    stdout[len(BranchPrefix):],
		Path:    stdout,
		gitRepo: repo,
	}, nil
}

// SetDefaultBranch sets default branch of repository.
func (repo *Repository) SetDefaultBranch(name string) error {
	_, err := NewCommand(repo.Ctx, "symbolic-ref", "HEAD", BranchPrefix+name).RunInDir(repo.Path)
	return err
}

// GetDefaultBranch gets default branch of repository.
func (repo *Repository) GetDefaultBranch() (string, error) {
	return NewCommand(repo.Ctx, "symbolic-ref", "HEAD").RunInDir(repo.Path)
}

// GetBranch returns a branch by it's name
func (repo *Repository) GetBranch(branch string) (*Branch, error) {
	if !repo.IsBranchExist(branch) {
		return nil, ErrBranchNotExist{branch}
	}
	return &Branch{
		Path:    repo.Path,
		Name:    branch,
		gitRepo: repo,
	}, nil
}

// GetBranchesByPath returns a branch by it's path
// if limit = 0 it will not limit
func GetBranchesByPath(ctx context.Context, path string, skip, limit int) ([]*Branch, int, error) {
	gitRepo, err := OpenRepositoryCtx(ctx, path)
	if err != nil {
		return nil, 0, err
	}
	defer gitRepo.Close()

	return gitRepo.GetBranches(skip, limit)
}

// GetBranches returns a slice of *git.Branch
func (repo *Repository) GetBranches(skip, limit int) ([]*Branch, int, error) {
	brs, countAll, err := repo.GetBranchNames(skip, limit)
	if err != nil {
		return nil, 0, err
	}

	branches := make([]*Branch, len(brs))
	for i := range brs {
		branches[i] = &Branch{
			Path:    repo.Path,
			Name:    brs[i],
			gitRepo: repo,
		}
	}

	return branches, countAll, nil
}

// DeleteBranchOptions Option(s) for delete branch
type DeleteBranchOptions struct {
	Force bool
}

// DeleteBranch delete a branch by name on repository.
func (repo *Repository) DeleteBranch(name string, opts DeleteBranchOptions) error {
	cmd := NewCommand(repo.Ctx, "branch")

	if opts.Force {
		cmd.AddArguments("-D")
	} else {
		cmd.AddArguments("-d")
	}

	cmd.AddArguments("--", name)
	_, err := cmd.RunInDir(repo.Path)

	return err
}

// CreateBranch create a new branch
func (repo *Repository) CreateBranch(branch, oldbranchOrCommit string) error {
	cmd := NewCommand(repo.Ctx, "branch")
	cmd.AddArguments("--", branch, oldbranchOrCommit)

	_, err := cmd.RunInDir(repo.Path)

	return err
}

// AddRemote adds a new remote to repository.
func (repo *Repository) AddRemote(name, url string, fetch bool) error {
	cmd := NewCommand(repo.Ctx, "remote", "add")
	if fetch {
		cmd.AddArguments("-f")
	}
	cmd.AddArguments(name, url)

	_, err := cmd.RunInDir(repo.Path)
	return err
}

// RemoveRemote removes a remote from repository.
func (repo *Repository) RemoveRemote(name string) error {
	_, err := NewCommand(repo.Ctx, "remote", "rm", name).RunInDir(repo.Path)
	return err
}

// GetCommit returns the head commit of a branch
func (branch *Branch) GetCommit() (*Commit, error) {
	return branch.gitRepo.GetBranchCommit(branch.Name)
}

// RenameBranch rename a branch
func (repo *Repository) RenameBranch(from, to string) error {
	_, err := NewCommand(repo.Ctx, "branch", "-m", from, to).RunInDir(repo.Path)
	return err
}
