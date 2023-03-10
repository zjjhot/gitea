// Copyright 2020 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"strings"

	"code.gitea.io/gitea/modules/git"
)

// PushUpdateOptions defines the push update options
type PushUpdateOptions struct {
	PusherID     int64
	PusherName   string
	RepoUserName string
	RepoName     string
	RefFullName  string // branch, tag or other name to push
	OldCommitID  string
	NewCommitID  string
}

// IsNewRef return true if it's a first-time push to a branch, tag or etc.
func (opts *PushUpdateOptions) IsNewRef() bool {
	return opts.OldCommitID == git.EmptySHA
}

// IsDelRef return true if it's a deletion to a branch or tag
func (opts *PushUpdateOptions) IsDelRef() bool {
	return opts.NewCommitID == git.EmptySHA
}

// IsUpdateRef return true if it's an update operation
func (opts *PushUpdateOptions) IsUpdateRef() bool {
	return !opts.IsNewRef() && !opts.IsDelRef()
}

// IsTag return true if it's an operation to a tag
func (opts *PushUpdateOptions) IsTag() bool {
	return strings.HasPrefix(opts.RefFullName, git.TagPrefix)
}

// IsNewTag return true if it's a creation to a tag
func (opts *PushUpdateOptions) IsNewTag() bool {
	return opts.IsTag() && opts.IsNewRef()
}

// IsDelTag return true if it's a deletion to a tag
func (opts *PushUpdateOptions) IsDelTag() bool {
	return opts.IsTag() && opts.IsDelRef()
}

// IsBranch return true if it's a push to branch
func (opts *PushUpdateOptions) IsBranch() bool {
	return strings.HasPrefix(opts.RefFullName, git.BranchPrefix)
}

// IsNewBranch return true if it's the first-time push to a branch
func (opts *PushUpdateOptions) IsNewBranch() bool {
	return opts.IsBranch() && opts.IsNewRef()
}

// IsUpdateBranch return true if it's not the first push to a branch
func (opts *PushUpdateOptions) IsUpdateBranch() bool {
	return opts.IsBranch() && opts.IsUpdateRef()
}

// IsDelBranch return true if it's a deletion to a branch
func (opts *PushUpdateOptions) IsDelBranch() bool {
	return opts.IsBranch() && opts.IsDelRef()
}

// TagName returns simple tag name if it's an operation to a tag
func (opts *PushUpdateOptions) TagName() string {
	return opts.RefFullName[len(git.TagPrefix):]
}

// BranchName returns simple branch name if it's an operation to branch
func (opts *PushUpdateOptions) BranchName() string {
	return opts.RefFullName[len(git.BranchPrefix):]
}

// RefName returns simple name for ref
func (opts *PushUpdateOptions) RefName() string {
	if strings.HasPrefix(opts.RefFullName, git.TagPrefix) {
		return opts.RefFullName[len(git.TagPrefix):]
	} else if strings.HasPrefix(opts.RefFullName, git.BranchPrefix) {
		return opts.RefFullName[len(git.BranchPrefix):]
	}
	return ""
}

// RepoFullName returns repo full name
func (opts *PushUpdateOptions) RepoFullName() string {
	return opts.RepoUserName + "/" + opts.RepoName
}
