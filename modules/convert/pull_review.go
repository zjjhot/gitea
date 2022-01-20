// Copyright 2020 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package convert

import (
	"context"
	"strings"

	"code.gitea.io/gitea/models"
	user_model "code.gitea.io/gitea/models/user"
	api "code.gitea.io/gitea/modules/structs"
)

// ToPullReview convert a review to api format
func ToPullReview(ctx context.Context, r *models.Review, doer *user_model.User) (*api.PullReview, error) {
	if err := r.LoadAttributes(ctx); err != nil {
		if !user_model.IsErrUserNotExist(err) {
			return nil, err
		}
		r.Reviewer = user_model.NewGhostUser()
	}

	result := &api.PullReview{
		ID:                r.ID,
		Reviewer:          ToUser(r.Reviewer, doer),
		ReviewerTeam:      ToTeam(r.ReviewerTeam),
		State:             api.ReviewStateUnknown,
		Body:              r.Content,
		CommitID:          r.CommitID,
		Stale:             r.Stale,
		Official:          r.Official,
		Dismissed:         r.Dismissed,
		CodeCommentsCount: r.GetCodeCommentsCount(),
		Submitted:         r.CreatedUnix.AsTime(),
		HTMLURL:           r.HTMLURL(),
		HTMLPullURL:       r.Issue.HTMLURL(),
	}

	switch r.Type {
	case models.ReviewTypeApprove:
		result.State = api.ReviewStateApproved
	case models.ReviewTypeReject:
		result.State = api.ReviewStateRequestChanges
	case models.ReviewTypeComment:
		result.State = api.ReviewStateComment
	case models.ReviewTypePending:
		result.State = api.ReviewStatePending
	case models.ReviewTypeRequest:
		result.State = api.ReviewStateRequestReview
	}

	return result, nil
}

// ToPullReviewList convert a list of review to it's api format
func ToPullReviewList(ctx context.Context, rl []*models.Review, doer *user_model.User) ([]*api.PullReview, error) {
	result := make([]*api.PullReview, 0, len(rl))
	for i := range rl {
		// show pending reviews only for the user who created them
		if rl[i].Type == models.ReviewTypePending && !(doer.IsAdmin || doer.ID == rl[i].ReviewerID) {
			continue
		}
		r, err := ToPullReview(ctx, rl[i], doer)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}

// ToPullReviewCommentList convert the CodeComments of an review to it's api format
func ToPullReviewCommentList(ctx context.Context, review *models.Review, doer *user_model.User) ([]*api.PullReviewComment, error) {
	if err := review.LoadAttributes(ctx); err != nil {
		if !user_model.IsErrUserNotExist(err) {
			return nil, err
		}
		review.Reviewer = user_model.NewGhostUser()
	}

	apiComments := make([]*api.PullReviewComment, 0, len(review.CodeComments))

	for _, lines := range review.CodeComments {
		for _, comments := range lines {
			for _, comment := range comments {
				apiComment := &api.PullReviewComment{
					ID:           comment.ID,
					Body:         comment.Content,
					Poster:       ToUser(comment.Poster, doer),
					Resolver:     ToUser(comment.ResolveDoer, doer),
					ReviewID:     review.ID,
					Created:      comment.CreatedUnix.AsTime(),
					Updated:      comment.UpdatedUnix.AsTime(),
					Path:         comment.TreePath,
					CommitID:     comment.CommitSHA,
					OrigCommitID: comment.OldRef,
					DiffHunk:     patch2diff(comment.Patch),
					HTMLURL:      comment.HTMLURL(),
					HTMLPullURL:  review.Issue.HTMLURL(),
				}

				if comment.Line < 0 {
					apiComment.OldLineNum = comment.UnsignedLine()
				} else {
					apiComment.LineNum = comment.UnsignedLine()
				}
				apiComments = append(apiComments, apiComment)
			}
		}
	}
	return apiComments, nil
}

func patch2diff(patch string) string {
	split := strings.Split(patch, "\n@@")
	if len(split) == 2 {
		return "@@" + split[1]
	}
	return ""
}
