// Copyright 2015 The Gogs Authors. All rights reserved.
// Copyright 2018 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build gogit
// +build gogit

package git

import (
	"context"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
)

// IsObjectExist returns true if given reference exists in the repository.
func (repo *Repository) IsObjectExist(name string) bool {
	if name == "" {
		return false
	}

	_, err := repo.gogitRepo.ResolveRevision(plumbing.Revision(name))

	return err == nil
}

// IsReferenceExist returns true if given reference exists in the repository.
func (repo *Repository) IsReferenceExist(name string) bool {
	if name == "" {
		return false
	}

	reference, err := repo.gogitRepo.Reference(plumbing.ReferenceName(name), true)
	if err != nil {
		return false
	}
	return reference.Type() != plumbing.InvalidReference
}

// IsBranchExist returns true if given branch exists in current repository.
func (repo *Repository) IsBranchExist(name string) bool {
	if name == "" {
		return false
	}
	reference, err := repo.gogitRepo.Reference(plumbing.ReferenceName(BranchPrefix+name), true)
	if err != nil {
		return false
	}
	return reference.Type() != plumbing.InvalidReference
}

// GetBranches returns branches from the repository, skipping skip initial branches and
// returning at most limit branches, or all branches if limit is 0.
func (repo *Repository) GetBranchNames(skip, limit int) ([]string, int, error) {
	var branchNames []string

	branches, err := repo.gogitRepo.Branches()
	if err != nil {
		return nil, 0, err
	}

	i := 0
	count := 0
	_ = branches.ForEach(func(branch *plumbing.Reference) error {
		count++
		if i < skip {
			i++
			return nil
		} else if limit != 0 && count > skip+limit {
			return nil
		}

		branchNames = append(branchNames, strings.TrimPrefix(branch.Name().String(), BranchPrefix))
		return nil
	})

	// TODO: Sort?

	return branchNames, count, nil
}

// WalkReferences walks all the references from the repository
func WalkReferences(ctx context.Context, repoPath string, walkfn func(string) error) (int, error) {
	repo := RepositoryFromContext(ctx, repoPath)
	if repo == nil {
		var err error
		repo, err = OpenRepositoryCtx(ctx, repoPath)
		if err != nil {
			return 0, err
		}
		defer repo.Close()
	}

	i := 0
	iter, err := repo.gogitRepo.References()
	if err != nil {
		return i, err
	}
	defer iter.Close()

	err = iter.ForEach(func(ref *plumbing.Reference) error {
		err := walkfn(string(ref.Name()))
		i++
		return err
	})
	return i, err
}
