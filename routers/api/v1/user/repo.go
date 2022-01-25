// Copyright 2017 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package user

import (
	"net/http"

	"code.gitea.io/gitea/models"
	"code.gitea.io/gitea/models/db"
	"code.gitea.io/gitea/models/perm"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/context"
	"code.gitea.io/gitea/modules/convert"
	api "code.gitea.io/gitea/modules/structs"
	"code.gitea.io/gitea/routers/api/v1/utils"
)

// listUserRepos - List the repositories owned by the given user.
func listUserRepos(ctx *context.APIContext, u *user_model.User, private bool) {
	opts := utils.GetListOptions(ctx)

	repos, count, err := models.GetUserRepositories(&models.SearchRepoOptions{
		Actor:       u,
		Private:     private,
		ListOptions: opts,
		OrderBy:     "id ASC",
	})
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "GetUserRepositories", err)
		return
	}

	if err := repos.LoadAttributes(); err != nil {
		ctx.Error(http.StatusInternalServerError, "RepositoryList.LoadAttributes", err)
		return
	}

	apiRepos := make([]*api.Repository, 0, len(repos))
	for i := range repos {
		access, err := models.AccessLevel(ctx.User, repos[i])
		if err != nil {
			ctx.Error(http.StatusInternalServerError, "AccessLevel", err)
			return
		}
		if ctx.IsSigned && ctx.User.IsAdmin || access >= perm.AccessModeRead {
			apiRepos = append(apiRepos, convert.ToRepo(repos[i], access))
		}
	}

	ctx.SetLinkHeader(int(count), opts.PageSize)
	ctx.SetTotalCountHeader(count)
	ctx.JSON(http.StatusOK, &apiRepos)
}

// ListUserRepos - list the repos owned by the given user.
func ListUserRepos(ctx *context.APIContext) {
	// swagger:operation GET /users/{username}/repos user userListRepos
	// ---
	// summary: List the repos owned by the given user
	// produces:
	// - application/json
	// parameters:
	// - name: username
	//   in: path
	//   description: username of user
	//   type: string
	//   required: true
	// - name: page
	//   in: query
	//   description: page number of results to return (1-based)
	//   type: integer
	// - name: limit
	//   in: query
	//   description: page size of results
	//   type: integer
	// responses:
	//   "200":
	//     "$ref": "#/responses/RepositoryList"

	user := GetUserByParams(ctx)
	if ctx.Written() {
		return
	}
	private := ctx.IsSigned
	listUserRepos(ctx, user, private)
}

// ListMyRepos - list the repositories you own or have access to.
func ListMyRepos(ctx *context.APIContext) {
	// swagger:operation GET /user/repos user userCurrentListRepos
	// ---
	// summary: List the repos that the authenticated user owns
	// produces:
	// - application/json
	// parameters:
	// - name: page
	//   in: query
	//   description: page number of results to return (1-based)
	//   type: integer
	// - name: limit
	//   in: query
	//   description: page size of results
	//   type: integer
	// responses:
	//   "200":
	//     "$ref": "#/responses/RepositoryList"

	opts := &models.SearchRepoOptions{
		ListOptions:        utils.GetListOptions(ctx),
		Actor:              ctx.User,
		OwnerID:            ctx.User.ID,
		Private:            ctx.IsSigned,
		IncludeDescription: true,
	}

	var err error
	repos, count, err := models.SearchRepository(opts)
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "SearchRepository", err)
		return
	}

	results := make([]*api.Repository, len(repos))
	for i, repo := range repos {
		if err = repo.GetOwner(db.DefaultContext); err != nil {
			ctx.Error(http.StatusInternalServerError, "GetOwner", err)
			return
		}
		accessMode, err := models.AccessLevel(ctx.User, repo)
		if err != nil {
			ctx.Error(http.StatusInternalServerError, "AccessLevel", err)
		}
		results[i] = convert.ToRepo(repo, accessMode)
	}

	ctx.SetLinkHeader(int(count), opts.ListOptions.PageSize)
	ctx.SetTotalCountHeader(count)
	ctx.JSON(http.StatusOK, &results)
}

// ListOrgRepos - list the repositories of an organization.
func ListOrgRepos(ctx *context.APIContext) {
	// swagger:operation GET /orgs/{org}/repos organization orgListRepos
	// ---
	// summary: List an organization's repos
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of the organization
	//   type: string
	//   required: true
	// - name: page
	//   in: query
	//   description: page number of results to return (1-based)
	//   type: integer
	// - name: limit
	//   in: query
	//   description: page size of results
	//   type: integer
	// responses:
	//   "200":
	//     "$ref": "#/responses/RepositoryList"

	listUserRepos(ctx, ctx.Org.Organization.AsUser(), ctx.IsSigned)
}
