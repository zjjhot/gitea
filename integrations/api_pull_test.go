// Copyright 2017 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package integrations

import (
	"fmt"
	"net/http"
	"testing"

	issues_model "code.gitea.io/gitea/models/issues"
	repo_model "code.gitea.io/gitea/models/repo"
	"code.gitea.io/gitea/models/unittest"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/setting"
	api "code.gitea.io/gitea/modules/structs"
	"code.gitea.io/gitea/services/forms"
	issue_service "code.gitea.io/gitea/services/issue"

	"github.com/stretchr/testify/assert"
)

func TestAPIViewPulls(t *testing.T) {
	defer prepareTestEnv(t)()
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1}).(*repo_model.Repository)
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: repo.OwnerID}).(*user_model.User)

	session := loginUser(t, "user2")
	token := getTokenForLoggedInUser(t, session)
	req := NewRequestf(t, "GET", "/api/v1/repos/%s/%s/pulls?state=all&token="+token, owner.Name, repo.Name)
	resp := session.MakeRequest(t, req, http.StatusOK)

	var pulls []*api.PullRequest
	DecodeJSON(t, resp, &pulls)
	expectedLen := unittest.GetCount(t, &issues_model.Issue{RepoID: repo.ID}, unittest.Cond("is_pull = ?", true))
	assert.Len(t, pulls, expectedLen)
}

// TestAPIMergePullWIP ensures that we can't merge a WIP pull request
func TestAPIMergePullWIP(t *testing.T) {
	defer prepareTestEnv(t)()
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1}).(*repo_model.Repository)
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: repo.OwnerID}).(*user_model.User)
	pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{Status: issues_model.PullRequestStatusMergeable}, unittest.Cond("has_merged = ?", false)).(*issues_model.PullRequest)
	pr.LoadIssue()
	issue_service.ChangeTitle(pr.Issue, owner, setting.Repository.PullRequest.WorkInProgressPrefixes[0]+" "+pr.Issue.Title)

	// force reload
	pr.LoadAttributes()

	assert.Contains(t, pr.Issue.Title, setting.Repository.PullRequest.WorkInProgressPrefixes[0])

	session := loginUser(t, owner.Name)
	token := getTokenForLoggedInUser(t, session)
	req := NewRequestWithJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%d/merge?token=%s", owner.Name, repo.Name, pr.Index, token), &forms.MergePullRequestForm{
		MergeMessageField: pr.Issue.Title,
		Do:                string(repo_model.MergeStyleMerge),
	})

	session.MakeRequest(t, req, http.StatusMethodNotAllowed)
}

func TestAPICreatePullSuccess(t *testing.T) {
	defer prepareTestEnv(t)()
	repo10 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 10}).(*repo_model.Repository)
	// repo10 have code, pulls units.
	repo11 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 11}).(*repo_model.Repository)
	// repo11 only have code unit but should still create pulls
	owner10 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: repo10.OwnerID}).(*user_model.User)
	owner11 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: repo11.OwnerID}).(*user_model.User)

	session := loginUser(t, owner11.Name)
	token := getTokenForLoggedInUser(t, session)
	req := NewRequestWithJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/repos/%s/%s/pulls?token=%s", owner10.Name, repo10.Name, token), &api.CreatePullRequestOption{
		Head:  fmt.Sprintf("%s:master", owner11.Name),
		Base:  "master",
		Title: "create a failure pr",
	})
	session.MakeRequest(t, req, http.StatusCreated)
	session.MakeRequest(t, req, http.StatusUnprocessableEntity) // second request should fail
}

func TestAPICreatePullWithFieldsSuccess(t *testing.T) {
	defer prepareTestEnv(t)()
	// repo10 have code, pulls units.
	repo10 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 10}).(*repo_model.Repository)
	owner10 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: repo10.OwnerID}).(*user_model.User)
	// repo11 only have code unit but should still create pulls
	repo11 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 11}).(*repo_model.Repository)
	owner11 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: repo11.OwnerID}).(*user_model.User)

	session := loginUser(t, owner11.Name)
	token := getTokenForLoggedInUser(t, session)

	opts := &api.CreatePullRequestOption{
		Head:      fmt.Sprintf("%s:master", owner11.Name),
		Base:      "master",
		Title:     "create a failure pr",
		Body:      "foobaaar",
		Milestone: 5,
		Assignees: []string{owner10.Name},
		Labels:    []int64{5},
	}

	req := NewRequestWithJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/repos/%s/%s/pulls?token=%s", owner10.Name, repo10.Name, token), opts)

	res := session.MakeRequest(t, req, http.StatusCreated)
	pull := new(api.PullRequest)
	DecodeJSON(t, res, pull)

	assert.NotNil(t, pull.Milestone)
	assert.EqualValues(t, opts.Milestone, pull.Milestone.ID)
	if assert.Len(t, pull.Assignees, 1) {
		assert.EqualValues(t, opts.Assignees[0], owner10.Name)
	}
	assert.NotNil(t, pull.Labels)
	assert.EqualValues(t, opts.Labels[0], pull.Labels[0].ID)
}

func TestAPICreatePullWithFieldsFailure(t *testing.T) {
	defer prepareTestEnv(t)()
	// repo10 have code, pulls units.
	repo10 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 10}).(*repo_model.Repository)
	owner10 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: repo10.OwnerID}).(*user_model.User)
	// repo11 only have code unit but should still create pulls
	repo11 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 11}).(*repo_model.Repository)
	owner11 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: repo11.OwnerID}).(*user_model.User)

	session := loginUser(t, owner11.Name)
	token := getTokenForLoggedInUser(t, session)

	opts := &api.CreatePullRequestOption{
		Head: fmt.Sprintf("%s:master", owner11.Name),
		Base: "master",
	}

	req := NewRequestWithJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/repos/%s/%s/pulls?token=%s", owner10.Name, repo10.Name, token), opts)
	session.MakeRequest(t, req, http.StatusUnprocessableEntity)
	opts.Title = "is required"

	opts.Milestone = 666
	session.MakeRequest(t, req, http.StatusUnprocessableEntity)
	opts.Milestone = 5

	opts.Assignees = []string{"qweruqweroiuyqweoiruywqer"}
	session.MakeRequest(t, req, http.StatusUnprocessableEntity)
	opts.Assignees = []string{owner10.LoginName}

	opts.Labels = []int64{55555}
	session.MakeRequest(t, req, http.StatusUnprocessableEntity)
	opts.Labels = []int64{5}
}

func TestAPIEditPull(t *testing.T) {
	defer prepareTestEnv(t)()
	repo10 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 10}).(*repo_model.Repository)
	owner10 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: repo10.OwnerID}).(*user_model.User)

	session := loginUser(t, owner10.Name)
	token := getTokenForLoggedInUser(t, session)
	req := NewRequestWithJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/repos/%s/%s/pulls?token=%s", owner10.Name, repo10.Name, token), &api.CreatePullRequestOption{
		Head:  "develop",
		Base:  "master",
		Title: "create a success pr",
	})
	pull := new(api.PullRequest)
	resp := session.MakeRequest(t, req, http.StatusCreated)
	DecodeJSON(t, resp, pull)
	assert.EqualValues(t, "master", pull.Base.Name)

	req = NewRequestWithJSON(t, http.MethodPatch, fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%d?token=%s", owner10.Name, repo10.Name, pull.Index, token), &api.EditPullRequestOption{
		Base:  "feature/1",
		Title: "edit a this pr",
	})
	resp = session.MakeRequest(t, req, http.StatusCreated)
	DecodeJSON(t, resp, pull)
	assert.EqualValues(t, "feature/1", pull.Base.Name)

	req = NewRequestWithJSON(t, http.MethodPatch, fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%d?token=%s", owner10.Name, repo10.Name, pull.Index, token), &api.EditPullRequestOption{
		Base: "not-exist",
	})
	session.MakeRequest(t, req, http.StatusNotFound)
}
