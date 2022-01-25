// Copyright 2014 The Gogs Authors. All rights reserved.
// Copyright 2018 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package repo

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"code.gitea.io/gitea/models"
	"code.gitea.io/gitea/models/db"
	"code.gitea.io/gitea/models/perm"
	repo_model "code.gitea.io/gitea/models/repo"
	unit_model "code.gitea.io/gitea/models/unit"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/context"
	"code.gitea.io/gitea/modules/convert"
	"code.gitea.io/gitea/modules/git"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/setting"
	api "code.gitea.io/gitea/modules/structs"
	"code.gitea.io/gitea/modules/util"
	"code.gitea.io/gitea/modules/validation"
	"code.gitea.io/gitea/modules/web"
	"code.gitea.io/gitea/routers/api/v1/utils"
	repo_service "code.gitea.io/gitea/services/repository"
)

var searchOrderByMap = map[string]map[string]db.SearchOrderBy{
	"asc": {
		"alpha":   db.SearchOrderByAlphabetically,
		"created": db.SearchOrderByOldest,
		"updated": db.SearchOrderByLeastUpdated,
		"size":    db.SearchOrderBySize,
		"id":      db.SearchOrderByID,
	},
	"desc": {
		"alpha":   db.SearchOrderByAlphabeticallyReverse,
		"created": db.SearchOrderByNewest,
		"updated": db.SearchOrderByRecentUpdated,
		"size":    db.SearchOrderBySizeReverse,
		"id":      db.SearchOrderByIDReverse,
	},
}

// Search repositories via options
func Search(ctx *context.APIContext) {
	// swagger:operation GET /repos/search repository repoSearch
	// ---
	// summary: Search for repositories
	// produces:
	// - application/json
	// parameters:
	// - name: q
	//   in: query
	//   description: keyword
	//   type: string
	// - name: topic
	//   in: query
	//   description: Limit search to repositories with keyword as topic
	//   type: boolean
	// - name: includeDesc
	//   in: query
	//   description: include search of keyword within repository description
	//   type: boolean
	// - name: uid
	//   in: query
	//   description: search only for repos that the user with the given id owns or contributes to
	//   type: integer
	//   format: int64
	// - name: priority_owner_id
	//   in: query
	//   description: repo owner to prioritize in the results
	//   type: integer
	//   format: int64
	// - name: team_id
	//   in: query
	//   description: search only for repos that belong to the given team id
	//   type: integer
	//   format: int64
	// - name: starredBy
	//   in: query
	//   description: search only for repos that the user with the given id has starred
	//   type: integer
	//   format: int64
	// - name: private
	//   in: query
	//   description: include private repositories this user has access to (defaults to true)
	//   type: boolean
	// - name: is_private
	//   in: query
	//   description: show only pubic, private or all repositories (defaults to all)
	//   type: boolean
	// - name: template
	//   in: query
	//   description: include template repositories this user has access to (defaults to true)
	//   type: boolean
	// - name: archived
	//   in: query
	//   description: show only archived, non-archived or all repositories (defaults to all)
	//   type: boolean
	// - name: mode
	//   in: query
	//   description: type of repository to search for. Supported values are
	//                "fork", "source", "mirror" and "collaborative"
	//   type: string
	// - name: exclusive
	//   in: query
	//   description: if `uid` is given, search only for repos that the user owns
	//   type: boolean
	// - name: sort
	//   in: query
	//   description: sort repos by attribute. Supported values are
	//                "alpha", "created", "updated", "size", and "id".
	//                Default is "alpha"
	//   type: string
	// - name: order
	//   in: query
	//   description: sort order, either "asc" (ascending) or "desc" (descending).
	//                Default is "asc", ignored if "sort" is not specified.
	//   type: string
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
	//     "$ref": "#/responses/SearchResults"
	//   "422":
	//     "$ref": "#/responses/validationError"

	opts := &models.SearchRepoOptions{
		ListOptions:        utils.GetListOptions(ctx),
		Actor:              ctx.User,
		Keyword:            ctx.FormTrim("q"),
		OwnerID:            ctx.FormInt64("uid"),
		PriorityOwnerID:    ctx.FormInt64("priority_owner_id"),
		TeamID:             ctx.FormInt64("team_id"),
		TopicOnly:          ctx.FormBool("topic"),
		Collaborate:        util.OptionalBoolNone,
		Private:            ctx.IsSigned && (ctx.FormString("private") == "" || ctx.FormBool("private")),
		Template:           util.OptionalBoolNone,
		StarredByID:        ctx.FormInt64("starredBy"),
		IncludeDescription: ctx.FormBool("includeDesc"),
	}

	if ctx.FormString("template") != "" {
		opts.Template = util.OptionalBoolOf(ctx.FormBool("template"))
	}

	if ctx.FormBool("exclusive") {
		opts.Collaborate = util.OptionalBoolFalse
	}

	mode := ctx.FormString("mode")
	switch mode {
	case "source":
		opts.Fork = util.OptionalBoolFalse
		opts.Mirror = util.OptionalBoolFalse
	case "fork":
		opts.Fork = util.OptionalBoolTrue
	case "mirror":
		opts.Mirror = util.OptionalBoolTrue
	case "collaborative":
		opts.Mirror = util.OptionalBoolFalse
		opts.Collaborate = util.OptionalBoolTrue
	case "":
	default:
		ctx.Error(http.StatusUnprocessableEntity, "", fmt.Errorf("Invalid search mode: \"%s\"", mode))
		return
	}

	if ctx.FormString("archived") != "" {
		opts.Archived = util.OptionalBoolOf(ctx.FormBool("archived"))
	}

	if ctx.FormString("is_private") != "" {
		opts.IsPrivate = util.OptionalBoolOf(ctx.FormBool("is_private"))
	}

	sortMode := ctx.FormString("sort")
	if len(sortMode) > 0 {
		sortOrder := ctx.FormString("order")
		if len(sortOrder) == 0 {
			sortOrder = "asc"
		}
		if searchModeMap, ok := searchOrderByMap[sortOrder]; ok {
			if orderBy, ok := searchModeMap[sortMode]; ok {
				opts.OrderBy = orderBy
			} else {
				ctx.Error(http.StatusUnprocessableEntity, "", fmt.Errorf("Invalid sort mode: \"%s\"", sortMode))
				return
			}
		} else {
			ctx.Error(http.StatusUnprocessableEntity, "", fmt.Errorf("Invalid sort order: \"%s\"", sortOrder))
			return
		}
	}

	var err error
	repos, count, err := models.SearchRepository(opts)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, api.SearchError{
			OK:    false,
			Error: err.Error(),
		})
		return
	}

	results := make([]*api.Repository, len(repos))
	for i, repo := range repos {
		if err = repo.GetOwner(db.DefaultContext); err != nil {
			ctx.JSON(http.StatusInternalServerError, api.SearchError{
				OK:    false,
				Error: err.Error(),
			})
			return
		}
		accessMode, err := models.AccessLevel(ctx.User, repo)
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, api.SearchError{
				OK:    false,
				Error: err.Error(),
			})
		}
		results[i] = convert.ToRepo(repo, accessMode)
	}

	ctx.SetLinkHeader(int(count), opts.PageSize)
	ctx.SetTotalCountHeader(count)
	ctx.JSON(http.StatusOK, api.SearchResults{
		OK:   true,
		Data: results,
	})
}

// CreateUserRepo create a repository for a user
func CreateUserRepo(ctx *context.APIContext, owner *user_model.User, opt api.CreateRepoOption) {
	if opt.AutoInit && opt.Readme == "" {
		opt.Readme = "Default"
	}
	repo, err := repo_service.CreateRepository(ctx.User, owner, models.CreateRepoOptions{
		Name:          opt.Name,
		Description:   opt.Description,
		IssueLabels:   opt.IssueLabels,
		Gitignores:    opt.Gitignores,
		License:       opt.License,
		Readme:        opt.Readme,
		IsPrivate:     opt.Private,
		AutoInit:      opt.AutoInit,
		DefaultBranch: opt.DefaultBranch,
		TrustModel:    repo_model.ToTrustModel(opt.TrustModel),
		IsTemplate:    opt.Template,
	})
	if err != nil {
		if repo_model.IsErrRepoAlreadyExist(err) {
			ctx.Error(http.StatusConflict, "", "The repository with the same name already exists.")
		} else if db.IsErrNameReserved(err) ||
			db.IsErrNamePatternNotAllowed(err) {
			ctx.Error(http.StatusUnprocessableEntity, "", err)
		} else {
			ctx.Error(http.StatusInternalServerError, "CreateRepository", err)
		}
		return
	}

	// reload repo from db to get a real state after creation
	repo, err = repo_model.GetRepositoryByID(repo.ID)
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "GetRepositoryByID", err)
	}

	ctx.JSON(http.StatusCreated, convert.ToRepo(repo, perm.AccessModeOwner))
}

// Create one repository of mine
func Create(ctx *context.APIContext) {
	// swagger:operation POST /user/repos repository user createCurrentUserRepo
	// ---
	// summary: Create a repository
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: body
	//   in: body
	//   schema:
	//     "$ref": "#/definitions/CreateRepoOption"
	// responses:
	//   "201":
	//     "$ref": "#/responses/Repository"
	//   "409":
	//     description: The repository with the same name already exists.
	//   "422":
	//     "$ref": "#/responses/validationError"
	opt := web.GetForm(ctx).(*api.CreateRepoOption)
	if ctx.User.IsOrganization() {
		// Shouldn't reach this condition, but just in case.
		ctx.Error(http.StatusUnprocessableEntity, "", "not allowed creating repository for organization")
		return
	}
	CreateUserRepo(ctx, ctx.User, *opt)
}

// Generate Create a repository using a template
func Generate(ctx *context.APIContext) {
	// swagger:operation POST /repos/{template_owner}/{template_repo}/generate repository generateRepo
	// ---
	// summary: Create a repository using a template
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: template_owner
	//   in: path
	//   description: name of the template repository owner
	//   type: string
	//   required: true
	// - name: template_repo
	//   in: path
	//   description: name of the template repository
	//   type: string
	//   required: true
	// - name: body
	//   in: body
	//   schema:
	//     "$ref": "#/definitions/GenerateRepoOption"
	// responses:
	//   "201":
	//     "$ref": "#/responses/Repository"
	//   "403":
	//     "$ref": "#/responses/forbidden"
	//   "404":
	//     "$ref": "#/responses/notFound"
	//   "409":
	//     description: The repository with the same name already exists.
	//   "422":
	//     "$ref": "#/responses/validationError"
	form := web.GetForm(ctx).(*api.GenerateRepoOption)

	if !ctx.Repo.Repository.IsTemplate {
		ctx.Error(http.StatusUnprocessableEntity, "", "this is not a template repo")
		return
	}

	if ctx.User.IsOrganization() {
		ctx.Error(http.StatusUnprocessableEntity, "", "not allowed creating repository for organization")
		return
	}

	opts := models.GenerateRepoOptions{
		Name:        form.Name,
		Description: form.Description,
		Private:     form.Private,
		GitContent:  form.GitContent,
		Topics:      form.Topics,
		GitHooks:    form.GitHooks,
		Webhooks:    form.Webhooks,
		Avatar:      form.Avatar,
		IssueLabels: form.Labels,
	}

	if !opts.IsValid() {
		ctx.Error(http.StatusUnprocessableEntity, "", "must select at least one template item")
		return
	}

	ctxUser := ctx.User
	var err error
	if form.Owner != ctxUser.Name {
		ctxUser, err = user_model.GetUserByName(form.Owner)
		if err != nil {
			if user_model.IsErrUserNotExist(err) {
				ctx.JSON(http.StatusNotFound, map[string]interface{}{
					"error": "request owner `" + form.Owner + "` does not exist",
				})
				return
			}

			ctx.Error(http.StatusInternalServerError, "GetUserByName", err)
			return
		}

		if !ctx.User.IsAdmin && !ctxUser.IsOrganization() {
			ctx.Error(http.StatusForbidden, "", "Only admin can generate repository for other user.")
			return
		}

		if !ctx.User.IsAdmin {
			canCreate, err := models.OrgFromUser(ctxUser).CanCreateOrgRepo(ctx.User.ID)
			if err != nil {
				ctx.ServerError("CanCreateOrgRepo", err)
				return
			} else if !canCreate {
				ctx.Error(http.StatusForbidden, "", "Given user is not allowed to create repository in organization.")
				return
			}
		}
	}

	repo, err := repo_service.GenerateRepository(ctx.User, ctxUser, ctx.Repo.Repository, opts)
	if err != nil {
		if repo_model.IsErrRepoAlreadyExist(err) {
			ctx.Error(http.StatusConflict, "", "The repository with the same name already exists.")
		} else if db.IsErrNameReserved(err) ||
			db.IsErrNamePatternNotAllowed(err) {
			ctx.Error(http.StatusUnprocessableEntity, "", err)
		} else {
			ctx.Error(http.StatusInternalServerError, "CreateRepository", err)
		}
		return
	}
	log.Trace("Repository generated [%d]: %s/%s", repo.ID, ctxUser.Name, repo.Name)

	ctx.JSON(http.StatusCreated, convert.ToRepo(repo, perm.AccessModeOwner))
}

// CreateOrgRepoDeprecated create one repository of the organization
func CreateOrgRepoDeprecated(ctx *context.APIContext) {
	// swagger:operation POST /org/{org}/repos organization createOrgRepoDeprecated
	// ---
	// summary: Create a repository in an organization
	// deprecated: true
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of organization
	//   type: string
	//   required: true
	// - name: body
	//   in: body
	//   schema:
	//     "$ref": "#/definitions/CreateRepoOption"
	// responses:
	//   "201":
	//     "$ref": "#/responses/Repository"
	//   "422":
	//     "$ref": "#/responses/validationError"
	//   "403":
	//     "$ref": "#/responses/forbidden"

	CreateOrgRepo(ctx)
}

// CreateOrgRepo create one repository of the organization
func CreateOrgRepo(ctx *context.APIContext) {
	// swagger:operation POST /orgs/{org}/repos organization createOrgRepo
	// ---
	// summary: Create a repository in an organization
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: org
	//   in: path
	//   description: name of organization
	//   type: string
	//   required: true
	// - name: body
	//   in: body
	//   schema:
	//     "$ref": "#/definitions/CreateRepoOption"
	// responses:
	//   "201":
	//     "$ref": "#/responses/Repository"
	//   "404":
	//     "$ref": "#/responses/notFound"
	//   "403":
	//     "$ref": "#/responses/forbidden"
	opt := web.GetForm(ctx).(*api.CreateRepoOption)
	org, err := models.GetOrgByName(ctx.Params(":org"))
	if err != nil {
		if models.IsErrOrgNotExist(err) {
			ctx.Error(http.StatusUnprocessableEntity, "", err)
		} else {
			ctx.Error(http.StatusInternalServerError, "GetOrgByName", err)
		}
		return
	}

	if !models.HasOrgOrUserVisible(org.AsUser(), ctx.User) {
		ctx.NotFound("HasOrgOrUserVisible", nil)
		return
	}

	if !ctx.User.IsAdmin {
		canCreate, err := org.CanCreateOrgRepo(ctx.User.ID)
		if err != nil {
			ctx.Error(http.StatusInternalServerError, "CanCreateOrgRepo", err)
			return
		} else if !canCreate {
			ctx.Error(http.StatusForbidden, "", "Given user is not allowed to create repository in organization.")
			return
		}
	}
	CreateUserRepo(ctx, org.AsUser(), *opt)
}

// Get one repository
func Get(ctx *context.APIContext) {
	// swagger:operation GET /repos/{owner}/{repo} repository repoGet
	// ---
	// summary: Get a repository
	// produces:
	// - application/json
	// parameters:
	// - name: owner
	//   in: path
	//   description: owner of the repo
	//   type: string
	//   required: true
	// - name: repo
	//   in: path
	//   description: name of the repo
	//   type: string
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/Repository"

	if err := ctx.Repo.Repository.LoadAttributes(ctx); err != nil {
		ctx.Error(http.StatusInternalServerError, "Repository.LoadAttributes", err)
		return
	}

	ctx.JSON(http.StatusOK, convert.ToRepo(ctx.Repo.Repository, ctx.Repo.AccessMode))
}

// GetByID returns a single Repository
func GetByID(ctx *context.APIContext) {
	// swagger:operation GET /repositories/{id} repository repoGetByID
	// ---
	// summary: Get a repository by id
	// produces:
	// - application/json
	// parameters:
	// - name: id
	//   in: path
	//   description: id of the repo to get
	//   type: integer
	//   format: int64
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/Repository"

	repo, err := repo_model.GetRepositoryByID(ctx.ParamsInt64(":id"))
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			ctx.NotFound()
		} else {
			ctx.Error(http.StatusInternalServerError, "GetRepositoryByID", err)
		}
		return
	}

	perm, err := models.GetUserRepoPermission(repo, ctx.User)
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "AccessLevel", err)
		return
	} else if !perm.HasAccess() {
		ctx.NotFound()
		return
	}
	ctx.JSON(http.StatusOK, convert.ToRepo(repo, perm.AccessMode))
}

// Edit edit repository properties
func Edit(ctx *context.APIContext) {
	// swagger:operation PATCH /repos/{owner}/{repo} repository repoEdit
	// ---
	// summary: Edit a repository's properties. Only fields that are set will be changed.
	// produces:
	// - application/json
	// parameters:
	// - name: owner
	//   in: path
	//   description: owner of the repo to edit
	//   type: string
	//   required: true
	// - name: repo
	//   in: path
	//   description: name of the repo to edit
	//   type: string
	//   required: true
	//   required: true
	// - name: body
	//   in: body
	//   description: "Properties of a repo that you can edit"
	//   schema:
	//     "$ref": "#/definitions/EditRepoOption"
	// responses:
	//   "200":
	//     "$ref": "#/responses/Repository"
	//   "403":
	//     "$ref": "#/responses/forbidden"
	//   "422":
	//     "$ref": "#/responses/validationError"

	opts := *web.GetForm(ctx).(*api.EditRepoOption)

	if err := updateBasicProperties(ctx, opts); err != nil {
		return
	}

	if err := updateRepoUnits(ctx, opts); err != nil {
		return
	}

	if opts.Archived != nil {
		if err := updateRepoArchivedState(ctx, opts); err != nil {
			return
		}
	}

	if opts.MirrorInterval != nil {
		if err := updateMirrorInterval(ctx, opts); err != nil {
			return
		}
	}

	repo, err := repo_model.GetRepositoryByID(ctx.Repo.Repository.ID)
	if err != nil {
		ctx.InternalServerError(err)
		return
	}

	ctx.JSON(http.StatusOK, convert.ToRepo(repo, ctx.Repo.AccessMode))
}

// updateBasicProperties updates the basic properties of a repo: Name, Description, Website and Visibility
func updateBasicProperties(ctx *context.APIContext, opts api.EditRepoOption) error {
	owner := ctx.Repo.Owner
	repo := ctx.Repo.Repository
	newRepoName := repo.Name
	if opts.Name != nil {
		newRepoName = *opts.Name
	}
	// Check if repository name has been changed and not just a case change
	if repo.LowerName != strings.ToLower(newRepoName) {
		if err := repo_service.ChangeRepositoryName(ctx.User, repo, newRepoName); err != nil {
			switch {
			case repo_model.IsErrRepoAlreadyExist(err):
				ctx.Error(http.StatusUnprocessableEntity, fmt.Sprintf("repo name is already taken [name: %s]", newRepoName), err)
			case db.IsErrNameReserved(err):
				ctx.Error(http.StatusUnprocessableEntity, fmt.Sprintf("repo name is reserved [name: %s]", newRepoName), err)
			case db.IsErrNamePatternNotAllowed(err):
				ctx.Error(http.StatusUnprocessableEntity, fmt.Sprintf("repo name's pattern is not allowed [name: %s, pattern: %s]", newRepoName, err.(db.ErrNamePatternNotAllowed).Pattern), err)
			default:
				ctx.Error(http.StatusUnprocessableEntity, "ChangeRepositoryName", err)
			}
			return err
		}

		log.Trace("Repository name changed: %s/%s -> %s", ctx.Repo.Owner.Name, repo.Name, newRepoName)
	}
	// Update the name in the repo object for the response
	repo.Name = newRepoName
	repo.LowerName = strings.ToLower(newRepoName)

	if opts.Description != nil {
		repo.Description = *opts.Description
	}

	if opts.Website != nil {
		repo.Website = *opts.Website
	}

	visibilityChanged := false
	if opts.Private != nil {
		// Visibility of forked repository is forced sync with base repository.
		if repo.IsFork {
			if err := repo.GetBaseRepo(); err != nil {
				ctx.Error(http.StatusInternalServerError, "Unable to load base repository", err)
				return err
			}
			*opts.Private = repo.BaseRepo.IsPrivate
		}

		visibilityChanged = repo.IsPrivate != *opts.Private
		// when ForcePrivate enabled, you could change public repo to private, but only admin users can change private to public
		if visibilityChanged && setting.Repository.ForcePrivate && !*opts.Private && !ctx.User.IsAdmin {
			err := fmt.Errorf("cannot change private repository to public")
			ctx.Error(http.StatusUnprocessableEntity, "Force Private enabled", err)
			return err
		}

		repo.IsPrivate = *opts.Private
	}

	if opts.Template != nil {
		repo.IsTemplate = *opts.Template
	}

	if ctx.Repo.GitRepo == nil && !repo.IsEmpty {
		var err error
		ctx.Repo.GitRepo, err = git.OpenRepositoryCtx(ctx, ctx.Repo.Repository.RepoPath())
		if err != nil {
			ctx.Error(http.StatusInternalServerError, "Unable to OpenRepository", err)
			return err
		}
		defer ctx.Repo.GitRepo.Close()
	}

	// Default branch only updated if changed and exist or the repository is empty
	if opts.DefaultBranch != nil && repo.DefaultBranch != *opts.DefaultBranch && (repo.IsEmpty || ctx.Repo.GitRepo.IsBranchExist(*opts.DefaultBranch)) {
		if !repo.IsEmpty {
			if err := ctx.Repo.GitRepo.SetDefaultBranch(*opts.DefaultBranch); err != nil {
				if !git.IsErrUnsupportedVersion(err) {
					ctx.Error(http.StatusInternalServerError, "SetDefaultBranch", err)
					return err
				}
			}
		}
		repo.DefaultBranch = *opts.DefaultBranch
	}

	if err := models.UpdateRepository(repo, visibilityChanged); err != nil {
		ctx.Error(http.StatusInternalServerError, "UpdateRepository", err)
		return err
	}

	log.Trace("Repository basic settings updated: %s/%s", owner.Name, repo.Name)
	return nil
}

// updateRepoUnits updates repo units: Issue settings, Wiki settings, PR settings
func updateRepoUnits(ctx *context.APIContext, opts api.EditRepoOption) error {
	owner := ctx.Repo.Owner
	repo := ctx.Repo.Repository

	var units []repo_model.RepoUnit
	var deleteUnitTypes []unit_model.Type

	if opts.HasIssues != nil {
		if *opts.HasIssues && opts.ExternalTracker != nil && !unit_model.TypeExternalTracker.UnitGlobalDisabled() {
			// Check that values are valid
			if !validation.IsValidExternalURL(opts.ExternalTracker.ExternalTrackerURL) {
				err := fmt.Errorf("External tracker URL not valid")
				ctx.Error(http.StatusUnprocessableEntity, "Invalid external tracker URL", err)
				return err
			}
			if len(opts.ExternalTracker.ExternalTrackerFormat) != 0 && !validation.IsValidExternalTrackerURLFormat(opts.ExternalTracker.ExternalTrackerFormat) {
				err := fmt.Errorf("External tracker URL format not valid")
				ctx.Error(http.StatusUnprocessableEntity, "Invalid external tracker URL format", err)
				return err
			}

			units = append(units, repo_model.RepoUnit{
				RepoID: repo.ID,
				Type:   unit_model.TypeExternalTracker,
				Config: &repo_model.ExternalTrackerConfig{
					ExternalTrackerURL:    opts.ExternalTracker.ExternalTrackerURL,
					ExternalTrackerFormat: opts.ExternalTracker.ExternalTrackerFormat,
					ExternalTrackerStyle:  opts.ExternalTracker.ExternalTrackerStyle,
				},
			})
			deleteUnitTypes = append(deleteUnitTypes, unit_model.TypeIssues)
		} else if *opts.HasIssues && opts.ExternalTracker == nil && !unit_model.TypeIssues.UnitGlobalDisabled() {
			// Default to built-in tracker
			var config *repo_model.IssuesConfig

			if opts.InternalTracker != nil {
				config = &repo_model.IssuesConfig{
					EnableTimetracker:                opts.InternalTracker.EnableTimeTracker,
					AllowOnlyContributorsToTrackTime: opts.InternalTracker.AllowOnlyContributorsToTrackTime,
					EnableDependencies:               opts.InternalTracker.EnableIssueDependencies,
				}
			} else if unit, err := repo.GetUnit(unit_model.TypeIssues); err != nil {
				// Unit type doesn't exist so we make a new config file with default values
				config = &repo_model.IssuesConfig{
					EnableTimetracker:                true,
					AllowOnlyContributorsToTrackTime: true,
					EnableDependencies:               true,
				}
			} else {
				config = unit.IssuesConfig()
			}

			units = append(units, repo_model.RepoUnit{
				RepoID: repo.ID,
				Type:   unit_model.TypeIssues,
				Config: config,
			})
			deleteUnitTypes = append(deleteUnitTypes, unit_model.TypeExternalTracker)
		} else if !*opts.HasIssues {
			if !unit_model.TypeExternalTracker.UnitGlobalDisabled() {
				deleteUnitTypes = append(deleteUnitTypes, unit_model.TypeExternalTracker)
			}
			if !unit_model.TypeIssues.UnitGlobalDisabled() {
				deleteUnitTypes = append(deleteUnitTypes, unit_model.TypeIssues)
			}
		}
	}

	if opts.HasWiki != nil {
		if *opts.HasWiki && opts.ExternalWiki != nil && !unit_model.TypeExternalWiki.UnitGlobalDisabled() {
			// Check that values are valid
			if !validation.IsValidExternalURL(opts.ExternalWiki.ExternalWikiURL) {
				err := fmt.Errorf("External wiki URL not valid")
				ctx.Error(http.StatusUnprocessableEntity, "", "Invalid external wiki URL")
				return err
			}

			units = append(units, repo_model.RepoUnit{
				RepoID: repo.ID,
				Type:   unit_model.TypeExternalWiki,
				Config: &repo_model.ExternalWikiConfig{
					ExternalWikiURL: opts.ExternalWiki.ExternalWikiURL,
				},
			})
			deleteUnitTypes = append(deleteUnitTypes, unit_model.TypeWiki)
		} else if *opts.HasWiki && opts.ExternalWiki == nil && !unit_model.TypeWiki.UnitGlobalDisabled() {
			config := &repo_model.UnitConfig{}
			units = append(units, repo_model.RepoUnit{
				RepoID: repo.ID,
				Type:   unit_model.TypeWiki,
				Config: config,
			})
			deleteUnitTypes = append(deleteUnitTypes, unit_model.TypeExternalWiki)
		} else if !*opts.HasWiki {
			if !unit_model.TypeExternalWiki.UnitGlobalDisabled() {
				deleteUnitTypes = append(deleteUnitTypes, unit_model.TypeExternalWiki)
			}
			if !unit_model.TypeWiki.UnitGlobalDisabled() {
				deleteUnitTypes = append(deleteUnitTypes, unit_model.TypeWiki)
			}
		}
	}

	if opts.HasPullRequests != nil {
		if *opts.HasPullRequests && !unit_model.TypePullRequests.UnitGlobalDisabled() {
			// We do allow setting individual PR settings through the API, so
			// we get the config settings and then set them
			// if those settings were provided in the opts.
			unit, err := repo.GetUnit(unit_model.TypePullRequests)
			var config *repo_model.PullRequestsConfig
			if err != nil {
				// Unit type doesn't exist so we make a new config file with default values
				config = &repo_model.PullRequestsConfig{
					IgnoreWhitespaceConflicts:     false,
					AllowMerge:                    true,
					AllowRebase:                   true,
					AllowRebaseMerge:              true,
					AllowSquash:                   true,
					AllowManualMerge:              true,
					AutodetectManualMerge:         false,
					DefaultDeleteBranchAfterMerge: false,
					DefaultMergeStyle:             repo_model.MergeStyleMerge,
				}
			} else {
				config = unit.PullRequestsConfig()
			}

			if opts.IgnoreWhitespaceConflicts != nil {
				config.IgnoreWhitespaceConflicts = *opts.IgnoreWhitespaceConflicts
			}
			if opts.AllowMerge != nil {
				config.AllowMerge = *opts.AllowMerge
			}
			if opts.AllowRebase != nil {
				config.AllowRebase = *opts.AllowRebase
			}
			if opts.AllowRebaseMerge != nil {
				config.AllowRebaseMerge = *opts.AllowRebaseMerge
			}
			if opts.AllowSquash != nil {
				config.AllowSquash = *opts.AllowSquash
			}
			if opts.AllowManualMerge != nil {
				config.AllowManualMerge = *opts.AllowManualMerge
			}
			if opts.AutodetectManualMerge != nil {
				config.AutodetectManualMerge = *opts.AutodetectManualMerge
			}
			if opts.DefaultDeleteBranchAfterMerge != nil {
				config.DefaultDeleteBranchAfterMerge = *opts.DefaultDeleteBranchAfterMerge
			}
			if opts.DefaultMergeStyle != nil {
				config.DefaultMergeStyle = repo_model.MergeStyle(*opts.DefaultMergeStyle)
			}

			units = append(units, repo_model.RepoUnit{
				RepoID: repo.ID,
				Type:   unit_model.TypePullRequests,
				Config: config,
			})
		} else if !*opts.HasPullRequests && !unit_model.TypePullRequests.UnitGlobalDisabled() {
			deleteUnitTypes = append(deleteUnitTypes, unit_model.TypePullRequests)
		}
	}

	if opts.HasProjects != nil && !unit_model.TypeProjects.UnitGlobalDisabled() {
		if *opts.HasProjects {
			units = append(units, repo_model.RepoUnit{
				RepoID: repo.ID,
				Type:   unit_model.TypeProjects,
			})
		} else {
			deleteUnitTypes = append(deleteUnitTypes, unit_model.TypeProjects)
		}
	}

	if err := repo_model.UpdateRepositoryUnits(repo, units, deleteUnitTypes); err != nil {
		ctx.Error(http.StatusInternalServerError, "UpdateRepositoryUnits", err)
		return err
	}

	log.Trace("Repository advanced settings updated: %s/%s", owner.Name, repo.Name)
	return nil
}

// updateRepoArchivedState updates repo's archive state
func updateRepoArchivedState(ctx *context.APIContext, opts api.EditRepoOption) error {
	repo := ctx.Repo.Repository
	// archive / un-archive
	if opts.Archived != nil {
		if repo.IsMirror {
			err := fmt.Errorf("repo is a mirror, cannot archive/un-archive")
			ctx.Error(http.StatusUnprocessableEntity, err.Error(), err)
			return err
		}
		if *opts.Archived {
			if err := repo_model.SetArchiveRepoState(repo, *opts.Archived); err != nil {
				log.Error("Tried to archive a repo: %s", err)
				ctx.Error(http.StatusInternalServerError, "ArchiveRepoState", err)
				return err
			}
			log.Trace("Repository was archived: %s/%s", ctx.Repo.Owner.Name, repo.Name)
		} else {
			if err := repo_model.SetArchiveRepoState(repo, *opts.Archived); err != nil {
				log.Error("Tried to un-archive a repo: %s", err)
				ctx.Error(http.StatusInternalServerError, "ArchiveRepoState", err)
				return err
			}
			log.Trace("Repository was un-archived: %s/%s", ctx.Repo.Owner.Name, repo.Name)
		}
	}
	return nil
}

// updateMirrorInterval updates the repo's mirror Interval
func updateMirrorInterval(ctx *context.APIContext, opts api.EditRepoOption) error {
	repo := ctx.Repo.Repository

	if opts.MirrorInterval != nil {
		if !repo.IsMirror {
			err := fmt.Errorf("repo is not a mirror, can not change mirror interval")
			ctx.Error(http.StatusUnprocessableEntity, err.Error(), err)
			return err
		}
		mirror, err := repo_model.GetMirrorByRepoID(repo.ID)
		if err != nil {
			log.Error("Failed to get mirror: %s", err)
			ctx.Error(http.StatusInternalServerError, "MirrorInterval", err)
			return err
		}
		if interval, err := time.ParseDuration(*opts.MirrorInterval); err == nil {
			mirror.Interval = interval
			mirror.Repo = repo
			if err := repo_model.UpdateMirror(mirror); err != nil {
				log.Error("Failed to Set Mirror Interval: %s", err)
				ctx.Error(http.StatusUnprocessableEntity, "MirrorInterval", err)
				return err
			}
			log.Trace("Repository %s/%s Mirror Interval was Updated to %s", ctx.Repo.Owner.Name, repo.Name, interval)
		} else {
			log.Error("Wrong format for MirrorInternal Sent: %s", err)
			ctx.Error(http.StatusUnprocessableEntity, "MirrorInterval", err)
			return err
		}
	}
	return nil
}

// Delete one repository
func Delete(ctx *context.APIContext) {
	// swagger:operation DELETE /repos/{owner}/{repo} repository repoDelete
	// ---
	// summary: Delete a repository
	// produces:
	// - application/json
	// parameters:
	// - name: owner
	//   in: path
	//   description: owner of the repo to delete
	//   type: string
	//   required: true
	// - name: repo
	//   in: path
	//   description: name of the repo to delete
	//   type: string
	//   required: true
	// responses:
	//   "204":
	//     "$ref": "#/responses/empty"
	//   "403":
	//     "$ref": "#/responses/forbidden"

	owner := ctx.Repo.Owner
	repo := ctx.Repo.Repository

	canDelete, err := models.CanUserDelete(repo, ctx.User)
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "CanUserDelete", err)
		return
	} else if !canDelete {
		ctx.Error(http.StatusForbidden, "", "Given user is not owner of organization.")
		return
	}

	if ctx.Repo.GitRepo != nil {
		ctx.Repo.GitRepo.Close()
	}

	if err := repo_service.DeleteRepository(ctx, ctx.User, repo, true); err != nil {
		ctx.Error(http.StatusInternalServerError, "DeleteRepository", err)
		return
	}

	log.Trace("Repository deleted: %s/%s", owner.Name, repo.Name)
	ctx.Status(http.StatusNoContent)
}

// GetIssueTemplates returns the issue templates for a repository
func GetIssueTemplates(ctx *context.APIContext) {
	// swagger:operation GET /repos/{owner}/{repo}/issue_templates repository repoGetIssueTemplates
	// ---
	// summary: Get available issue templates for a repository
	// produces:
	// - application/json
	// parameters:
	// - name: owner
	//   in: path
	//   description: owner of the repo
	//   type: string
	//   required: true
	// - name: repo
	//   in: path
	//   description: name of the repo
	//   type: string
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/IssueTemplates"

	ctx.JSON(http.StatusOK, ctx.IssueTemplatesFromDefaultBranch())
}
