// Copyright 2014 The Gogs Authors. All rights reserved.
// Copyright 2019 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package user

import (
	"bytes"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"code.gitea.io/gitea/models"
	asymkey_model "code.gitea.io/gitea/models/asymkey"
	"code.gitea.io/gitea/models/db"
	repo_model "code.gitea.io/gitea/models/repo"
	"code.gitea.io/gitea/models/unit"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/base"
	"code.gitea.io/gitea/modules/context"
	issue_indexer "code.gitea.io/gitea/modules/indexer/issues"
	"code.gitea.io/gitea/modules/json"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/markup"
	"code.gitea.io/gitea/modules/markup/markdown"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/modules/util"
	"code.gitea.io/gitea/routers/web/feed"
	issue_service "code.gitea.io/gitea/services/issue"
	pull_service "code.gitea.io/gitea/services/pull"

	"github.com/keybase/go-crypto/openpgp"
	"github.com/keybase/go-crypto/openpgp/armor"
	"xorm.io/builder"
)

const (
	tplDashboard  base.TplName = "user/dashboard/dashboard"
	tplIssues     base.TplName = "user/dashboard/issues"
	tplMilestones base.TplName = "user/dashboard/milestones"
	tplProfile    base.TplName = "user/profile"
)

// getDashboardContextUser finds out which context user dashboard is being viewed as .
func getDashboardContextUser(ctx *context.Context) *user_model.User {
	ctxUser := ctx.User
	orgName := ctx.Params(":org")
	if len(orgName) > 0 {
		ctxUser = ctx.Org.Organization.AsUser()
		ctx.Data["Teams"] = ctx.Org.Teams
	}
	ctx.Data["ContextUser"] = ctxUser

	orgs, err := models.GetUserOrgsList(ctx.User)
	if err != nil {
		ctx.ServerError("GetUserOrgsList", err)
		return nil
	}
	ctx.Data["Orgs"] = orgs

	return ctxUser
}

// Dashboard render the dashboard page
func Dashboard(ctx *context.Context) {
	ctxUser := getDashboardContextUser(ctx)
	if ctx.Written() {
		return
	}

	ctx.Data["Title"] = ctxUser.DisplayName() + " - " + ctx.Tr("dashboard")
	ctx.Data["PageIsDashboard"] = true
	ctx.Data["PageIsNews"] = true
	cnt, _ := models.GetOrganizationCount(db.DefaultContext, ctxUser)
	ctx.Data["UserOrgsCount"] = cnt

	var uid int64
	if ctxUser != nil {
		uid = ctxUser.ID
	}

	ctx.PageData["dashboardRepoList"] = map[string]interface{}{
		"searchLimit": setting.UI.User.RepoPagingNum,
		"uid":         uid,
	}

	if setting.Service.EnableUserHeatmap {
		data, err := models.GetUserHeatmapDataByUserTeam(ctxUser, ctx.Org.Team, ctx.User)
		if err != nil {
			ctx.ServerError("GetUserHeatmapDataByUserTeam", err)
			return
		}
		ctx.Data["HeatmapData"] = data
	}

	var err error
	var mirrors []*repo_model.Repository
	if ctxUser.IsOrganization() {
		var env models.AccessibleReposEnvironment
		if ctx.Org.Team != nil {
			env = models.OrgFromUser(ctxUser).AccessibleTeamReposEnv(ctx.Org.Team)
		} else {
			env, err = models.OrgFromUser(ctxUser).AccessibleReposEnv(ctx.User.ID)
			if err != nil {
				ctx.ServerError("AccessibleReposEnv", err)
				return
			}
		}
		mirrors, err = env.MirrorRepos()
		if err != nil {
			ctx.ServerError("env.MirrorRepos", err)
			return
		}
	} else {
		mirrors, err = repo_model.GetUserMirrorRepositories(ctxUser.ID)
		if err != nil {
			ctx.ServerError("GetUserMirrorRepositories", err)
			return
		}
	}
	ctx.Data["MaxShowRepoNum"] = setting.UI.User.RepoPagingNum

	if err := repo_model.MirrorRepositoryList(mirrors).LoadAttributes(); err != nil {
		ctx.ServerError("MirrorRepositoryList.LoadAttributes", err)
		return
	}
	ctx.Data["MirrorCount"] = len(mirrors)
	ctx.Data["Mirrors"] = mirrors

	ctx.Data["Feeds"] = feed.RetrieveFeeds(ctx, models.GetFeedsOptions{
		RequestedUser:   ctxUser,
		RequestedTeam:   ctx.Org.Team,
		Actor:           ctx.User,
		IncludePrivate:  true,
		OnlyPerformedBy: false,
		IncludeDeleted:  false,
		Date:            ctx.FormString("date"),
	})

	if ctx.Written() {
		return
	}

	ctx.HTML(http.StatusOK, tplDashboard)
}

// Milestones render the user milestones page
func Milestones(ctx *context.Context) {
	if unit.TypeIssues.UnitGlobalDisabled() && unit.TypePullRequests.UnitGlobalDisabled() {
		log.Debug("Milestones overview page not available as both issues and pull requests are globally disabled")
		ctx.Status(404)
		return
	}

	ctx.Data["Title"] = ctx.Tr("milestones")
	ctx.Data["PageIsMilestonesDashboard"] = true

	ctxUser := getDashboardContextUser(ctx)
	if ctx.Written() {
		return
	}

	repoOpts := models.SearchRepoOptions{
		Actor:         ctxUser,
		OwnerID:       ctxUser.ID,
		Private:       true,
		AllPublic:     false,                 // Include also all public repositories of users and public organisations
		AllLimited:    false,                 // Include also all public repositories of limited organisations
		HasMilestones: util.OptionalBoolTrue, // Just needs display repos has milestones
	}

	if ctxUser.IsOrganization() && ctx.Org.Team != nil {
		repoOpts.TeamID = ctx.Org.Team.ID
	}

	var (
		userRepoCond = models.SearchRepositoryCondition(&repoOpts) // all repo condition user could visit
		repoCond     = userRepoCond
		repoIDs      []int64

		reposQuery   = ctx.FormString("repos")
		isShowClosed = ctx.FormString("state") == "closed"
		sortType     = ctx.FormString("sort")
		page         = ctx.FormInt("page")
		keyword      = ctx.FormTrim("q")
	)

	if page <= 1 {
		page = 1
	}

	if len(reposQuery) != 0 {
		if issueReposQueryPattern.MatchString(reposQuery) {
			// remove "[" and "]" from string
			reposQuery = reposQuery[1 : len(reposQuery)-1]
			// for each ID (delimiter ",") add to int to repoIDs

			for _, rID := range strings.Split(reposQuery, ",") {
				// Ensure nonempty string entries
				if rID != "" && rID != "0" {
					rIDint64, err := strconv.ParseInt(rID, 10, 64)
					// If the repo id specified by query is not parseable or not accessible by user, just ignore it.
					if err == nil {
						repoIDs = append(repoIDs, rIDint64)
					}
				}
			}
			if len(repoIDs) > 0 {
				// Don't just let repoCond = builder.In("id", repoIDs) because user may has no permission on repoIDs
				// But the original repoCond has a limitation
				repoCond = repoCond.And(builder.In("id", repoIDs))
			}
		} else {
			log.Warn("issueReposQueryPattern not match with query")
		}
	}

	counts, err := models.CountMilestonesByRepoCondAndKw(userRepoCond, keyword, isShowClosed)
	if err != nil {
		ctx.ServerError("CountMilestonesByRepoIDs", err)
		return
	}

	milestones, err := models.SearchMilestones(repoCond, page, isShowClosed, sortType, keyword)
	if err != nil {
		ctx.ServerError("SearchMilestones", err)
		return
	}

	showRepos, _, err := models.SearchRepositoryByCondition(&repoOpts, userRepoCond, false)
	if err != nil {
		ctx.ServerError("SearchRepositoryByCondition", err)
		return
	}
	sort.Sort(showRepos)

	for i := 0; i < len(milestones); {
		for _, repo := range showRepos {
			if milestones[i].RepoID == repo.ID {
				milestones[i].Repo = repo
				break
			}
		}
		if milestones[i].Repo == nil {
			log.Warn("Cannot find milestone %d 's repository %d", milestones[i].ID, milestones[i].RepoID)
			milestones = append(milestones[:i], milestones[i+1:]...)
			continue
		}

		milestones[i].RenderedContent, err = markdown.RenderString(&markup.RenderContext{
			URLPrefix: milestones[i].Repo.Link(),
			Metas:     milestones[i].Repo.ComposeMetas(),
			Ctx:       ctx,
		}, milestones[i].Content)
		if err != nil {
			ctx.ServerError("RenderString", err)
			return
		}

		if milestones[i].Repo.IsTimetrackerEnabled() {
			err := milestones[i].LoadTotalTrackedTime()
			if err != nil {
				ctx.ServerError("LoadTotalTrackedTime", err)
				return
			}
		}
		i++
	}

	milestoneStats, err := models.GetMilestonesStatsByRepoCondAndKw(repoCond, keyword)
	if err != nil {
		ctx.ServerError("GetMilestoneStats", err)
		return
	}

	var totalMilestoneStats *models.MilestonesStats
	if len(repoIDs) == 0 {
		totalMilestoneStats = milestoneStats
	} else {
		totalMilestoneStats, err = models.GetMilestonesStatsByRepoCondAndKw(userRepoCond, keyword)
		if err != nil {
			ctx.ServerError("GetMilestoneStats", err)
			return
		}
	}

	var pagerCount int
	if isShowClosed {
		ctx.Data["State"] = "closed"
		ctx.Data["Total"] = totalMilestoneStats.ClosedCount
		pagerCount = int(milestoneStats.ClosedCount)
	} else {
		ctx.Data["State"] = "open"
		ctx.Data["Total"] = totalMilestoneStats.OpenCount
		pagerCount = int(milestoneStats.OpenCount)
	}

	ctx.Data["Milestones"] = milestones
	ctx.Data["Repos"] = showRepos
	ctx.Data["Counts"] = counts
	ctx.Data["MilestoneStats"] = milestoneStats
	ctx.Data["SortType"] = sortType
	ctx.Data["Keyword"] = keyword
	if milestoneStats.Total() != totalMilestoneStats.Total() {
		ctx.Data["RepoIDs"] = repoIDs
	}
	ctx.Data["IsShowClosed"] = isShowClosed

	pager := context.NewPagination(pagerCount, setting.UI.IssuePagingNum, page, 5)
	pager.AddParam(ctx, "q", "Keyword")
	pager.AddParam(ctx, "repos", "RepoIDs")
	pager.AddParam(ctx, "sort", "SortType")
	pager.AddParam(ctx, "state", "State")
	ctx.Data["Page"] = pager

	ctx.HTML(http.StatusOK, tplMilestones)
}

// Pulls renders the user's pull request overview page
func Pulls(ctx *context.Context) {
	if unit.TypePullRequests.UnitGlobalDisabled() {
		log.Debug("Pull request overview page not available as it is globally disabled.")
		ctx.Status(404)
		return
	}

	ctx.Data["Title"] = ctx.Tr("pull_requests")
	ctx.Data["PageIsPulls"] = true
	buildIssueOverview(ctx, unit.TypePullRequests)
}

// Issues renders the user's issues overview page
func Issues(ctx *context.Context) {
	if unit.TypeIssues.UnitGlobalDisabled() {
		log.Debug("Issues overview page not available as it is globally disabled.")
		ctx.Status(404)
		return
	}

	ctx.Data["Title"] = ctx.Tr("issues")
	ctx.Data["PageIsIssues"] = true
	buildIssueOverview(ctx, unit.TypeIssues)
}

// Regexp for repos query
var issueReposQueryPattern = regexp.MustCompile(`^\[\d+(,\d+)*,?\]$`)

func buildIssueOverview(ctx *context.Context, unitType unit.Type) {
	// ----------------------------------------------------
	// Determine user; can be either user or organization.
	// Return with NotFound or ServerError if unsuccessful.
	// ----------------------------------------------------

	ctxUser := getDashboardContextUser(ctx)
	if ctx.Written() {
		return
	}

	var (
		viewType   string
		sortType   = ctx.FormString("sort")
		filterMode = models.FilterModeAll
	)

	// --------------------------------------------------------------------------------
	// Distinguish User from Organization.
	// Org:
	// - Remember pre-determined viewType string for later. Will be posted to ctx.Data.
	//   Organization does not have view type and filter mode.
	// User:
	// - Use ctx.FormString("type") to determine filterMode.
	//  The type is set when clicking for example "assigned to me" on the overview page.
	// - Remember either this or a fallback. Will be posted to ctx.Data.
	// --------------------------------------------------------------------------------

	// TODO: distinguish during routing

	viewType = ctx.FormString("type")
	switch viewType {
	case "assigned":
		filterMode = models.FilterModeAssign
	case "created_by":
		filterMode = models.FilterModeCreate
	case "mentioned":
		filterMode = models.FilterModeMention
	case "review_requested":
		filterMode = models.FilterModeReviewRequested
	case "your_repositories": // filterMode already set to All
	default:
		viewType = "your_repositories"
	}

	// --------------------------------------------------------------------------
	// Build opts (IssuesOptions), which contains filter information.
	// Will eventually be used to retrieve issues relevant for the overview page.
	// Note: Non-final states of opts are used in-between, namely for:
	//       - Keyword search
	//       - Count Issues by repo
	// --------------------------------------------------------------------------

	// Get repository IDs where User/Org/Team has access.
	var team *models.Team
	var org *models.Organization
	if ctx.Org != nil {
		org = ctx.Org.Organization
		team = ctx.Org.Team
	}

	isPullList := unitType == unit.TypePullRequests
	opts := &models.IssuesOptions{
		IsPull:     util.OptionalBoolOf(isPullList),
		SortType:   sortType,
		IsArchived: util.OptionalBoolFalse,
		Org:        org,
		Team:       team,
		User:       ctx.User,
	}

	switch filterMode {
	case models.FilterModeAll:
	case models.FilterModeAssign:
		opts.AssigneeID = ctx.User.ID
	case models.FilterModeCreate:
		opts.PosterID = ctx.User.ID
	case models.FilterModeMention:
		opts.MentionedID = ctx.User.ID
	case models.FilterModeReviewRequested:
		opts.ReviewRequestedID = ctx.User.ID
	}

	// keyword holds the search term entered into the search field.
	keyword := strings.Trim(ctx.FormString("q"), " ")
	ctx.Data["Keyword"] = keyword

	// Execute keyword search for issues.
	// USING NON-FINAL STATE OF opts FOR A QUERY.
	issueIDsFromSearch, err := issueIDsFromSearch(ctx, ctxUser, keyword, opts)
	if err != nil {
		ctx.ServerError("issueIDsFromSearch", err)
		return
	}

	// Ensure no issues are returned if a keyword was provided that didn't match any issues.
	var forceEmpty bool

	if len(issueIDsFromSearch) > 0 {
		opts.IssueIDs = issueIDsFromSearch
	} else if len(keyword) > 0 {
		forceEmpty = true
	}

	// Educated guess: Do or don't show closed issues.
	isShowClosed := ctx.FormString("state") == "closed"
	opts.IsClosed = util.OptionalBoolOf(isShowClosed)

	// Filter repos and count issues in them. Count will be used later.
	// USING NON-FINAL STATE OF opts FOR A QUERY.
	var issueCountByRepo map[int64]int64
	if !forceEmpty {
		issueCountByRepo, err = models.CountIssuesByRepo(opts)
		if err != nil {
			ctx.ServerError("CountIssuesByRepo", err)
			return
		}
	}

	// Make sure page number is at least 1. Will be posted to ctx.Data.
	page := ctx.FormInt("page")
	if page <= 1 {
		page = 1
	}
	opts.Page = page
	opts.PageSize = setting.UI.IssuePagingNum

	// Get IDs for labels (a filter option for issues/pulls).
	// Required for IssuesOptions.
	var labelIDs []int64
	selectedLabels := ctx.FormString("labels")
	if len(selectedLabels) > 0 && selectedLabels != "0" {
		labelIDs, err = base.StringsToInt64s(strings.Split(selectedLabels, ","))
		if err != nil {
			ctx.ServerError("StringsToInt64s", err)
			return
		}
	}
	opts.LabelIDs = labelIDs

	// Parse ctx.FormString("repos") and remember matched repo IDs for later.
	// Gets set when clicking filters on the issues overview page.
	repoIDs := getRepoIDs(ctx.FormString("repos"))
	if len(repoIDs) > 0 {
		opts.RepoIDs = repoIDs
	}

	// ------------------------------
	// Get issues as defined by opts.
	// ------------------------------

	// Slice of Issues that will be displayed on the overview page
	// USING FINAL STATE OF opts FOR A QUERY.
	var issues []*models.Issue
	if !forceEmpty {
		issues, err = models.Issues(opts)
		if err != nil {
			ctx.ServerError("Issues", err)
			return
		}
	} else {
		issues = []*models.Issue{}
	}

	// ----------------------------------
	// Add repository pointers to Issues.
	// ----------------------------------

	// showReposMap maps repository IDs to their Repository pointers.
	showReposMap, err := loadRepoByIDs(ctxUser, issueCountByRepo, unitType)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			ctx.NotFound("GetRepositoryByID", err)
			return
		}
		ctx.ServerError("loadRepoByIDs", err)
		return
	}

	// a RepositoryList
	showRepos := models.RepositoryListOfMap(showReposMap)
	sort.Sort(showRepos)

	// maps pull request IDs to their CommitStatus. Will be posted to ctx.Data.
	for _, issue := range issues {
		if issue.Repo == nil {
			issue.Repo = showReposMap[issue.RepoID]
		}
	}

	commitStatus, err := pull_service.GetIssuesLastCommitStatus(ctx, issues)
	if err != nil {
		ctx.ServerError("GetIssuesLastCommitStatus", err)
		return
	}

	// -------------------------------
	// Fill stats to post to ctx.Data.
	// -------------------------------
	var issueStats *models.IssueStats
	if !forceEmpty {
		statsOpts := models.UserIssueStatsOptions{
			UserID:     ctx.User.ID,
			FilterMode: filterMode,
			IsPull:     isPullList,
			IsClosed:   isShowClosed,
			IssueIDs:   issueIDsFromSearch,
			IsArchived: util.OptionalBoolFalse,
			LabelIDs:   opts.LabelIDs,
			Org:        org,
			Team:       team,
		}
		if len(repoIDs) > 0 {
			statsOpts.RepoIDs = repoIDs
		}
		issueStats, err = models.GetUserIssueStats(statsOpts)
		if err != nil {
			ctx.ServerError("GetUserIssueStats Shown", err)
			return
		}
	} else {
		issueStats = &models.IssueStats{}
	}

	// Will be posted to ctx.Data.
	var shownIssues int
	if !isShowClosed {
		shownIssues = int(issueStats.OpenCount)
		ctx.Data["TotalIssueCount"] = shownIssues
	} else {
		shownIssues = int(issueStats.ClosedCount)
		ctx.Data["TotalIssueCount"] = shownIssues
	}

	ctx.Data["IsShowClosed"] = isShowClosed

	ctx.Data["IssueRefEndNames"], ctx.Data["IssueRefURLs"] = issue_service.GetRefEndNamesAndURLs(issues, ctx.FormString("RepoLink"))

	ctx.Data["Issues"] = issues

	approvalCounts, err := models.IssueList(issues).GetApprovalCounts()
	if err != nil {
		ctx.ServerError("ApprovalCounts", err)
		return
	}
	ctx.Data["ApprovalCounts"] = func(issueID int64, typ string) int64 {
		counts, ok := approvalCounts[issueID]
		if !ok || len(counts) == 0 {
			return 0
		}
		reviewTyp := models.ReviewTypeApprove
		if typ == "reject" {
			reviewTyp = models.ReviewTypeReject
		} else if typ == "waiting" {
			reviewTyp = models.ReviewTypeRequest
		}
		for _, count := range counts {
			if count.Type == reviewTyp {
				return count.Count
			}
		}
		return 0
	}
	ctx.Data["CommitStatus"] = commitStatus
	ctx.Data["Repos"] = showRepos
	ctx.Data["Counts"] = issueCountByRepo
	ctx.Data["IssueStats"] = issueStats
	ctx.Data["ViewType"] = viewType
	ctx.Data["SortType"] = sortType
	ctx.Data["RepoIDs"] = repoIDs
	ctx.Data["IsShowClosed"] = isShowClosed
	ctx.Data["SelectLabels"] = selectedLabels

	if isShowClosed {
		ctx.Data["State"] = "closed"
	} else {
		ctx.Data["State"] = "open"
	}

	// Convert []int64 to string
	reposParam, _ := json.Marshal(repoIDs)

	ctx.Data["ReposParam"] = string(reposParam)

	pager := context.NewPagination(shownIssues, setting.UI.IssuePagingNum, page, 5)
	pager.AddParam(ctx, "q", "Keyword")
	pager.AddParam(ctx, "type", "ViewType")
	pager.AddParam(ctx, "repos", "ReposParam")
	pager.AddParam(ctx, "sort", "SortType")
	pager.AddParam(ctx, "state", "State")
	pager.AddParam(ctx, "labels", "SelectLabels")
	pager.AddParam(ctx, "milestone", "MilestoneID")
	pager.AddParam(ctx, "assignee", "AssigneeID")
	ctx.Data["Page"] = pager

	ctx.HTML(http.StatusOK, tplIssues)
}

func getRepoIDs(reposQuery string) []int64 {
	if len(reposQuery) == 0 || reposQuery == "[]" {
		return []int64{}
	}
	if !issueReposQueryPattern.MatchString(reposQuery) {
		log.Warn("issueReposQueryPattern does not match query")
		return []int64{}
	}

	var repoIDs []int64
	// remove "[" and "]" from string
	reposQuery = reposQuery[1 : len(reposQuery)-1]
	// for each ID (delimiter ",") add to int to repoIDs
	for _, rID := range strings.Split(reposQuery, ",") {
		// Ensure nonempty string entries
		if rID != "" && rID != "0" {
			rIDint64, err := strconv.ParseInt(rID, 10, 64)
			if err == nil {
				repoIDs = append(repoIDs, rIDint64)
			}
		}
	}

	return repoIDs
}

func issueIDsFromSearch(ctx *context.Context, ctxUser *user_model.User, keyword string, opts *models.IssuesOptions) ([]int64, error) {
	if len(keyword) == 0 {
		return []int64{}, nil
	}

	searchRepoIDs, err := models.GetRepoIDsForIssuesOptions(opts, ctxUser)
	if err != nil {
		return nil, fmt.Errorf("GetRepoIDsForIssuesOptions: %v", err)
	}
	issueIDsFromSearch, err := issue_indexer.SearchIssuesByKeyword(ctx, searchRepoIDs, keyword)
	if err != nil {
		return nil, fmt.Errorf("SearchIssuesByKeyword: %v", err)
	}

	return issueIDsFromSearch, nil
}

func loadRepoByIDs(ctxUser *user_model.User, issueCountByRepo map[int64]int64, unitType unit.Type) (map[int64]*repo_model.Repository, error) {
	totalRes := make(map[int64]*repo_model.Repository, len(issueCountByRepo))
	repoIDs := make([]int64, 0, 500)
	for id := range issueCountByRepo {
		if id <= 0 {
			continue
		}
		repoIDs = append(repoIDs, id)
		if len(repoIDs) == 500 {
			if err := repo_model.FindReposMapByIDs(repoIDs, totalRes); err != nil {
				return nil, err
			}
			repoIDs = repoIDs[:0]
		}
	}
	if len(repoIDs) > 0 {
		if err := repo_model.FindReposMapByIDs(repoIDs, totalRes); err != nil {
			return nil, err
		}
	}
	return totalRes, nil
}

// ShowSSHKeys output all the ssh keys of user by uid
func ShowSSHKeys(ctx *context.Context, uid int64) {
	keys, err := asymkey_model.ListPublicKeys(uid, db.ListOptions{})
	if err != nil {
		ctx.ServerError("ListPublicKeys", err)
		return
	}

	var buf bytes.Buffer
	for i := range keys {
		buf.WriteString(keys[i].OmitEmail())
		buf.WriteString("\n")
	}
	ctx.PlainTextBytes(http.StatusOK, buf.Bytes())
}

// ShowGPGKeys output all the public GPG keys of user by uid
func ShowGPGKeys(ctx *context.Context, uid int64) {
	keys, err := asymkey_model.ListGPGKeys(db.DefaultContext, uid, db.ListOptions{})
	if err != nil {
		ctx.ServerError("ListGPGKeys", err)
		return
	}
	entities := make([]*openpgp.Entity, 0)
	failedEntitiesID := make([]string, 0)
	for _, k := range keys {
		e, err := asymkey_model.GPGKeyToEntity(k)
		if err != nil {
			if asymkey_model.IsErrGPGKeyImportNotExist(err) {
				failedEntitiesID = append(failedEntitiesID, k.KeyID)
				continue // Skip previous import without backup of imported armored key
			}
			ctx.ServerError("ShowGPGKeys", err)
			return
		}
		entities = append(entities, e)
	}
	var buf bytes.Buffer

	headers := make(map[string]string)
	if len(failedEntitiesID) > 0 { // If some key need re-import to be exported
		headers["Note"] = fmt.Sprintf("The keys with the following IDs couldn't be exported and need to be reuploaded %s", strings.Join(failedEntitiesID, ", "))
	}
	writer, _ := armor.Encode(&buf, "PGP PUBLIC KEY BLOCK", headers)
	for _, e := range entities {
		err = e.Serialize(writer) // TODO find why key are exported with a different cipherTypeByte as original (should not be blocking but strange)
		if err != nil {
			ctx.ServerError("ShowGPGKeys", err)
			return
		}
	}
	writer.Close()
	ctx.PlainTextBytes(http.StatusOK, buf.Bytes())
}
