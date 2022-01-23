// Copyright 2022 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package webhook

import (
	"testing"

	webhook_model "code.gitea.io/gitea/models/webhook"
	api "code.gitea.io/gitea/modules/structs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackagistPayload(t *testing.T) {
	t.Run("Create", func(t *testing.T) {
		p := createTestPayload()

		d := new(PackagistPayload)
		pl, err := d.Create(p)
		require.NoError(t, err)
		require.Nil(t, pl)
	})

	t.Run("Delete", func(t *testing.T) {
		p := deleteTestPayload()

		d := new(PackagistPayload)
		pl, err := d.Delete(p)
		require.NoError(t, err)
		require.Nil(t, pl)
	})

	t.Run("Fork", func(t *testing.T) {
		p := forkTestPayload()

		d := new(PackagistPayload)
		pl, err := d.Fork(p)
		require.NoError(t, err)
		require.Nil(t, pl)
	})

	t.Run("Push", func(t *testing.T) {
		p := pushTestPayload()

		d := new(PackagistPayload)
		d.PackagistRepository.URL = "https://packagist.org/api/update-package?username=THEUSERNAME&apiToken=TOPSECRETAPITOKEN"
		pl, err := d.Push(p)
		require.NoError(t, err)
		require.NotNil(t, pl)
		require.IsType(t, &PackagistPayload{}, pl)

		assert.Equal(t, "https://packagist.org/api/update-package?username=THEUSERNAME&apiToken=TOPSECRETAPITOKEN", pl.(*PackagistPayload).PackagistRepository.URL)
	})

	t.Run("Issue", func(t *testing.T) {
		p := issueTestPayload()

		d := new(PackagistPayload)
		p.Action = api.HookIssueOpened
		pl, err := d.Issue(p)
		require.NoError(t, err)
		require.Nil(t, pl)

		p.Action = api.HookIssueClosed
		pl, err = d.Issue(p)
		require.NoError(t, err)
		require.Nil(t, pl)
	})

	t.Run("IssueComment", func(t *testing.T) {
		p := issueCommentTestPayload()

		d := new(PackagistPayload)
		pl, err := d.IssueComment(p)
		require.NoError(t, err)
		require.Nil(t, pl)
	})

	t.Run("PullRequest", func(t *testing.T) {
		p := pullRequestTestPayload()

		d := new(PackagistPayload)
		pl, err := d.PullRequest(p)
		require.NoError(t, err)
		require.Nil(t, pl)
	})

	t.Run("PullRequestComment", func(t *testing.T) {
		p := pullRequestCommentTestPayload()

		d := new(PackagistPayload)
		pl, err := d.IssueComment(p)
		require.NoError(t, err)
		require.Nil(t, pl)
	})

	t.Run("Review", func(t *testing.T) {
		p := pullRequestTestPayload()
		p.Action = api.HookIssueReviewed

		d := new(PackagistPayload)
		pl, err := d.Review(p, webhook_model.HookEventPullRequestReviewApproved)
		require.NoError(t, err)
		require.Nil(t, pl)
	})

	t.Run("Repository", func(t *testing.T) {
		p := repositoryTestPayload()

		d := new(PackagistPayload)
		pl, err := d.Repository(p)
		require.NoError(t, err)
		require.Nil(t, pl)
	})

	t.Run("Release", func(t *testing.T) {
		p := pullReleaseTestPayload()

		d := new(PackagistPayload)
		pl, err := d.Release(p)
		require.NoError(t, err)
		require.Nil(t, pl)
	})
}

func TestPackagistJSONPayload(t *testing.T) {
	p := pushTestPayload()

	pl, err := new(PackagistPayload).Push(p)
	require.NoError(t, err)
	require.NotNil(t, pl)
	require.IsType(t, &PackagistPayload{}, pl)

	json, err := pl.JSONPayload()
	require.NoError(t, err)
	assert.NotEmpty(t, json)
}
