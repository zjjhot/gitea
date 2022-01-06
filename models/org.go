// Copyright 2014 The Gogs Authors. All rights reserved.
// Copyright 2019 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package models

import (
	"context"
	"fmt"
	"strings"

	"code.gitea.io/gitea/models/db"
	"code.gitea.io/gitea/models/perm"
	repo_model "code.gitea.io/gitea/models/repo"
	"code.gitea.io/gitea/models/unit"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/modules/structs"

	"xorm.io/builder"
)

// Organization represents an organization
type Organization user_model.User

// OrgFromUser converts user to organization
func OrgFromUser(user *user_model.User) *Organization {
	return (*Organization)(user)
}

// TableName represents the real table name of Organization
func (Organization) TableName() string {
	return "user"
}

// IsOwnedBy returns true if given user is in the owner team.
func (org *Organization) IsOwnedBy(uid int64) (bool, error) {
	return IsOrganizationOwner(org.ID, uid)
}

// IsOrgMember returns true if given user is member of organization.
func (org *Organization) IsOrgMember(uid int64) (bool, error) {
	return IsOrganizationMember(org.ID, uid)
}

// CanCreateOrgRepo returns true if given user can create repo in organization
func (org *Organization) CanCreateOrgRepo(uid int64) (bool, error) {
	return CanCreateOrgRepo(org.ID, uid)
}

func (org *Organization) getTeam(e db.Engine, name string) (*Team, error) {
	return getTeam(e, org.ID, name)
}

// GetTeam returns named team of organization.
func (org *Organization) GetTeam(name string) (*Team, error) {
	return org.getTeam(db.GetEngine(db.DefaultContext), name)
}

func (org *Organization) getOwnerTeam(e db.Engine) (*Team, error) {
	return org.getTeam(e, ownerTeamName)
}

// GetOwnerTeam returns owner team of organization.
func (org *Organization) GetOwnerTeam() (*Team, error) {
	return org.getOwnerTeam(db.GetEngine(db.DefaultContext))
}

func (org *Organization) loadTeams(e db.Engine) ([]*Team, error) {
	var teams []*Team
	return teams, e.
		Where("org_id=?", org.ID).
		OrderBy("CASE WHEN name LIKE '" + ownerTeamName + "' THEN '' ELSE name END").
		Find(&teams)
}

// LoadTeams load teams if not loaded.
func (org *Organization) LoadTeams() ([]*Team, error) {
	return org.loadTeams(db.GetEngine(db.DefaultContext))
}

// GetMembers returns all members of organization.
func (org *Organization) GetMembers() (user_model.UserList, map[int64]bool, error) {
	return FindOrgMembers(&FindOrgMembersOpts{
		OrgID: org.ID,
	})
}

// HasMemberWithUserID returns true if user with userID is part of the u organisation.
func (org *Organization) HasMemberWithUserID(userID int64) bool {
	return org.hasMemberWithUserID(db.GetEngine(db.DefaultContext), userID)
}

func (org *Organization) hasMemberWithUserID(e db.Engine, userID int64) bool {
	isMember, err := isOrganizationMember(e, org.ID, userID)
	if err != nil {
		log.Error("IsOrganizationMember: %v", err)
		return false
	}
	return isMember
}

// AvatarLink returns the full avatar link with http host
func (org *Organization) AvatarLink() string {
	return org.AsUser().AvatarLink()
}

// HTMLURL returns the organization's full link.
func (org *Organization) HTMLURL() string {
	return org.AsUser().HTMLURL()
}

// OrganisationLink returns the organization sub page link.
func (org *Organization) OrganisationLink() string {
	return org.AsUser().OrganisationLink()
}

// ShortName ellipses username to length
func (org *Organization) ShortName(length int) string {
	return org.AsUser().ShortName(length)
}

// HomeLink returns the user or organization home page link.
func (org *Organization) HomeLink() string {
	return org.AsUser().HomeLink()
}

// CanCreateRepo returns if user login can create a repository
// NOTE: functions calling this assume a failure due to repository count limit; if new checks are added, those functions should be revised
func (org *Organization) CanCreateRepo() bool {
	return org.AsUser().CanCreateRepo()
}

// FindOrgMembersOpts represensts find org members conditions
type FindOrgMembersOpts struct {
	db.ListOptions
	OrgID      int64
	PublicOnly bool
}

// CountOrgMembers counts the organization's members
func CountOrgMembers(opts *FindOrgMembersOpts) (int64, error) {
	sess := db.GetEngine(db.DefaultContext).Where("org_id=?", opts.OrgID)
	if opts.PublicOnly {
		sess.And("is_public = ?", true)
	}
	return sess.Count(new(OrgUser))
}

// FindOrgMembers loads organization members according conditions
func FindOrgMembers(opts *FindOrgMembersOpts) (user_model.UserList, map[int64]bool, error) {
	ous, err := GetOrgUsersByOrgID(opts)
	if err != nil {
		return nil, nil, err
	}

	ids := make([]int64, len(ous))
	idsIsPublic := make(map[int64]bool, len(ous))
	for i, ou := range ous {
		ids[i] = ou.UID
		idsIsPublic[ou.UID] = ou.IsPublic
	}

	users, err := user_model.GetUsersByIDs(ids)
	if err != nil {
		return nil, nil, err
	}
	return users, idsIsPublic, nil
}

// AddMember adds new member to organization.
func (org *Organization) AddMember(uid int64) error {
	return AddOrgUser(org.ID, uid)
}

// RemoveMember removes member from organization.
func (org *Organization) RemoveMember(uid int64) error {
	return RemoveOrgUser(org.ID, uid)
}

func (org *Organization) removeOrgRepo(e db.Engine, repoID int64) error {
	return removeOrgRepo(e, org.ID, repoID)
}

// RemoveOrgRepo removes all team-repository relations of organization.
func (org *Organization) RemoveOrgRepo(repoID int64) error {
	return org.removeOrgRepo(db.GetEngine(db.DefaultContext), repoID)
}

// AsUser returns the org as user object
func (org *Organization) AsUser() *user_model.User {
	return (*user_model.User)(org)
}

// DisplayName returns full name if it's not empty,
// returns username otherwise.
func (org *Organization) DisplayName() string {
	return org.AsUser().DisplayName()
}

// CustomAvatarRelativePath returns user custom avatar relative path.
func (org *Organization) CustomAvatarRelativePath() string {
	return org.Avatar
}

// CreateOrganization creates record of a new organization.
func CreateOrganization(org *Organization, owner *user_model.User) (err error) {
	if !owner.CanCreateOrganization() {
		return ErrUserNotAllowedCreateOrg{}
	}

	if err = user_model.IsUsableUsername(org.Name); err != nil {
		return err
	}

	isExist, err := user_model.IsUserExist(0, org.Name)
	if err != nil {
		return err
	} else if isExist {
		return user_model.ErrUserAlreadyExist{Name: org.Name}
	}

	org.LowerName = strings.ToLower(org.Name)
	if org.Rands, err = user_model.GetUserSalt(); err != nil {
		return err
	}
	if org.Salt, err = user_model.GetUserSalt(); err != nil {
		return err
	}
	org.UseCustomAvatar = true
	org.MaxRepoCreation = -1
	org.NumTeams = 1
	org.NumMembers = 1
	org.Type = user_model.UserTypeOrganization

	ctx, committer, err := db.TxContext()
	if err != nil {
		return err
	}
	defer committer.Close()

	if err = user_model.DeleteUserRedirect(ctx, org.Name); err != nil {
		return err
	}

	if err = db.Insert(ctx, org); err != nil {
		return fmt.Errorf("insert organization: %v", err)
	}
	if err = user_model.GenerateRandomAvatarCtx(ctx, org.AsUser()); err != nil {
		return fmt.Errorf("generate random avatar: %v", err)
	}

	// Add initial creator to organization and owner team.
	if err = db.Insert(ctx, &OrgUser{
		UID:   owner.ID,
		OrgID: org.ID,
	}); err != nil {
		return fmt.Errorf("insert org-user relation: %v", err)
	}

	// Create default owner team.
	t := &Team{
		OrgID:                   org.ID,
		LowerName:               strings.ToLower(ownerTeamName),
		Name:                    ownerTeamName,
		AccessMode:              perm.AccessModeOwner,
		NumMembers:              1,
		IncludesAllRepositories: true,
		CanCreateOrgRepo:        true,
	}
	if err = db.Insert(ctx, t); err != nil {
		return fmt.Errorf("insert owner team: %v", err)
	}

	// insert units for team
	units := make([]TeamUnit, 0, len(unit.AllRepoUnitTypes))
	for _, tp := range unit.AllRepoUnitTypes {
		units = append(units, TeamUnit{
			OrgID:  org.ID,
			TeamID: t.ID,
			Type:   tp,
		})
	}

	if err = db.Insert(ctx, &units); err != nil {
		return err
	}

	if err = db.Insert(ctx, &TeamUser{
		UID:    owner.ID,
		OrgID:  org.ID,
		TeamID: t.ID,
	}); err != nil {
		return fmt.Errorf("insert team-user relation: %v", err)
	}

	return committer.Commit()
}

// GetOrgByName returns organization by given name.
func GetOrgByName(name string) (*Organization, error) {
	if len(name) == 0 {
		return nil, ErrOrgNotExist{0, name}
	}
	u := &Organization{
		LowerName: strings.ToLower(name),
		Type:      user_model.UserTypeOrganization,
	}
	has, err := db.GetEngine(db.DefaultContext).Get(u)
	if err != nil {
		return nil, err
	} else if !has {
		return nil, ErrOrgNotExist{0, name}
	}
	return u, nil
}

// CountOrganizations returns number of organizations.
func CountOrganizations() int64 {
	count, _ := db.GetEngine(db.DefaultContext).
		Where("type=1").
		Count(new(Organization))
	return count
}

// DeleteOrganization deletes models associated to an organization.
func DeleteOrganization(ctx context.Context, org *Organization) error {
	if org.Type != user_model.UserTypeOrganization {
		return fmt.Errorf("%s is a user not an organization", org.Name)
	}

	e := db.GetEngine(ctx)

	if err := deleteBeans(e,
		&Team{OrgID: org.ID},
		&OrgUser{OrgID: org.ID},
		&TeamUser{OrgID: org.ID},
		&TeamUnit{OrgID: org.ID},
	); err != nil {
		return fmt.Errorf("deleteBeans: %v", err)
	}

	if _, err := e.ID(org.ID).Delete(new(user_model.User)); err != nil {
		return fmt.Errorf("Delete: %v", err)
	}

	return nil
}

// ________                ____ ___
// \_____  \_______  ____ |    |   \______ ___________
//  /   |   \_  __ \/ ___\|    |   /  ___// __ \_  __ \
// /    |    \  | \/ /_/  >    |  /\___ \\  ___/|  | \/
// \_______  /__|  \___  /|______//____  >\___  >__|
//         \/     /_____/              \/     \/

// OrgUser represents an organization-user relation.
type OrgUser struct {
	ID       int64 `xorm:"pk autoincr"`
	UID      int64 `xorm:"INDEX UNIQUE(s)"`
	OrgID    int64 `xorm:"INDEX UNIQUE(s)"`
	IsPublic bool  `xorm:"INDEX"`
}

func init() {
	db.RegisterModel(new(OrgUser))
}

func isOrganizationOwner(e db.Engine, orgID, uid int64) (bool, error) {
	ownerTeam, err := getOwnerTeam(e, orgID)
	if err != nil {
		if IsErrTeamNotExist(err) {
			log.Error("Organization does not have owner team: %d", orgID)
			return false, nil
		}
		return false, err
	}
	return isTeamMember(e, orgID, ownerTeam.ID, uid)
}

// IsOrganizationOwner returns true if given user is in the owner team.
func IsOrganizationOwner(orgID, uid int64) (bool, error) {
	return isOrganizationOwner(db.GetEngine(db.DefaultContext), orgID, uid)
}

// IsOrganizationMember returns true if given user is member of organization.
func IsOrganizationMember(orgID, uid int64) (bool, error) {
	return isOrganizationMember(db.GetEngine(db.DefaultContext), orgID, uid)
}

func isOrganizationMember(e db.Engine, orgID, uid int64) (bool, error) {
	return e.
		Where("uid=?", uid).
		And("org_id=?", orgID).
		Table("org_user").
		Exist()
}

// IsPublicMembership returns true if given user public his/her membership.
func IsPublicMembership(orgID, uid int64) (bool, error) {
	return db.GetEngine(db.DefaultContext).
		Where("uid=?", uid).
		And("org_id=?", orgID).
		And("is_public=?", true).
		Table("org_user").
		Exist()
}

// CanCreateOrgRepo returns true if user can create repo in organization
func CanCreateOrgRepo(orgID, uid int64) (bool, error) {
	if owner, err := IsOrganizationOwner(orgID, uid); owner || err != nil {
		return owner, err
	}
	return db.GetEngine(db.DefaultContext).
		Where(builder.Eq{"team.can_create_org_repo": true}).
		Join("INNER", "team_user", "team_user.team_id = team.id").
		And("team_user.uid = ?", uid).
		And("team_user.org_id = ?", orgID).
		Exist(new(Team))
}

// GetOrgUserMaxAuthorizeLevel returns highest authorize level of user in an organization
func (org *Organization) GetOrgUserMaxAuthorizeLevel(uid int64) (perm.AccessMode, error) {
	var authorize perm.AccessMode
	_, err := db.GetEngine(db.DefaultContext).
		Select("max(team.authorize)").
		Table("team").
		Join("INNER", "team_user", "team_user.team_id = team.id").
		Where("team_user.uid = ?", uid).
		And("team_user.org_id = ?", org.ID).
		Get(&authorize)
	return authorize, err
}

// GetUsersWhoCanCreateOrgRepo returns users which are able to create repo in organization
func GetUsersWhoCanCreateOrgRepo(orgID int64) ([]*user_model.User, error) {
	return getUsersWhoCanCreateOrgRepo(db.GetEngine(db.DefaultContext), orgID)
}

func getUsersWhoCanCreateOrgRepo(e db.Engine, orgID int64) ([]*user_model.User, error) {
	users := make([]*user_model.User, 0, 10)
	return users, e.
		Join("INNER", "`team_user`", "`team_user`.uid=`user`.id").
		Join("INNER", "`team`", "`team`.id=`team_user`.team_id").
		Where(builder.Eq{"team.can_create_org_repo": true}.Or(builder.Eq{"team.authorize": perm.AccessModeOwner})).
		And("team_user.org_id = ?", orgID).Asc("`user`.name").Find(&users)
}

// MinimalOrg represents a simple orgnization with only needed columns
type MinimalOrg = Organization

// GetUserOrgsList returns one user's all orgs list
func GetUserOrgsList(user *user_model.User) ([]*MinimalOrg, error) {
	schema, err := db.TableInfo(new(user_model.User))
	if err != nil {
		return nil, err
	}

	outputCols := []string{
		"id",
		"name",
		"full_name",
		"visibility",
		"avatar",
		"avatar_email",
		"use_custom_avatar",
	}

	groupByCols := &strings.Builder{}
	for _, col := range outputCols {
		fmt.Fprintf(groupByCols, "`%s`.%s,", schema.Name, col)
	}
	groupByStr := groupByCols.String()
	groupByStr = groupByStr[0 : len(groupByStr)-1]

	sess := db.GetEngine(db.DefaultContext)
	sess = sess.Select(groupByStr+", count(distinct repo_id) as org_count").
		Table("user").
		Join("INNER", "team", "`team`.org_id = `user`.id").
		Join("INNER", "team_user", "`team`.id = `team_user`.team_id").
		Join("LEFT", builder.
			Select("id as repo_id, owner_id as repo_owner_id").
			From("repository").
			Where(accessibleRepositoryCondition(user)), "`repository`.repo_owner_id = `team`.org_id").
		Where("`team_user`.uid = ?", user.ID).
		GroupBy(groupByStr)

	type OrgCount struct {
		Organization `xorm:"extends"`
		OrgCount     int
	}

	orgCounts := make([]*OrgCount, 0, 10)

	if err := sess.
		Asc("`user`.name").
		Find(&orgCounts); err != nil {
		return nil, err
	}

	orgs := make([]*MinimalOrg, len(orgCounts))
	for i, orgCount := range orgCounts {
		orgCount.Organization.NumRepos = orgCount.OrgCount
		orgs[i] = &orgCount.Organization
	}

	return orgs, nil
}

// SearchOrganizationsOptions options to filter organizations
type SearchOrganizationsOptions struct {
	db.ListOptions
	All bool
}

// FindOrgOptions finds orgs options
type FindOrgOptions struct {
	db.ListOptions
	UserID         int64
	IncludePrivate bool
}

func queryUserOrgIDs(userID int64, includePrivate bool) *builder.Builder {
	cond := builder.Eq{"uid": userID}
	if !includePrivate {
		cond["is_public"] = true
	}
	return builder.Select("org_id").From("org_user").Where(cond)
}

func (opts FindOrgOptions) toConds() builder.Cond {
	cond := builder.NewCond()
	if opts.UserID > 0 {
		cond = cond.And(builder.In("`user`.`id`", queryUserOrgIDs(opts.UserID, opts.IncludePrivate)))
	}
	if !opts.IncludePrivate {
		cond = cond.And(builder.Eq{"`user`.visibility": structs.VisibleTypePublic})
	}
	return cond
}

// FindOrgs returns a list of organizations according given conditions
func FindOrgs(opts FindOrgOptions) ([]*Organization, error) {
	orgs := make([]*Organization, 0, 10)
	sess := db.GetEngine(db.DefaultContext).
		Where(opts.toConds()).
		Asc("`user`.name")
	if opts.Page > 0 && opts.PageSize > 0 {
		sess.Limit(opts.PageSize, opts.PageSize*(opts.Page-1))
	}
	return orgs, sess.Find(&orgs)
}

// CountOrgs returns total count organizations according options
func CountOrgs(opts FindOrgOptions) (int64, error) {
	return db.GetEngine(db.DefaultContext).
		Where(opts.toConds()).
		Count(new(user_model.User))
}

func getOwnedOrgsByUserID(sess db.Engine, userID int64) ([]*Organization, error) {
	orgs := make([]*Organization, 0, 10)
	return orgs, sess.
		Join("INNER", "`team_user`", "`team_user`.org_id=`user`.id").
		Join("INNER", "`team`", "`team`.id=`team_user`.team_id").
		Where("`team_user`.uid=?", userID).
		And("`team`.authorize=?", perm.AccessModeOwner).
		Asc("`user`.name").
		Find(&orgs)
}

// HasOrgOrUserVisible tells if the given user can see the given org or user
func HasOrgOrUserVisible(org, user *user_model.User) bool {
	return hasOrgOrUserVisible(db.GetEngine(db.DefaultContext), org, user)
}

func hasOrgOrUserVisible(e db.Engine, orgOrUser, user *user_model.User) bool {
	// Not SignedUser
	if user == nil {
		return orgOrUser.Visibility == structs.VisibleTypePublic
	}

	if user.IsAdmin || orgOrUser.ID == user.ID {
		return true
	}

	if (orgOrUser.Visibility == structs.VisibleTypePrivate || user.IsRestricted) && !OrgFromUser(orgOrUser).hasMemberWithUserID(e, user.ID) {
		return false
	}
	return true
}

// HasOrgsVisible tells if the given user can see at least one of the orgs provided
func HasOrgsVisible(orgs []*Organization, user *user_model.User) bool {
	if len(orgs) == 0 {
		return false
	}

	for _, org := range orgs {
		if HasOrgOrUserVisible(org.AsUser(), user) {
			return true
		}
	}
	return false
}

// GetOwnedOrgsByUserID returns a list of organizations are owned by given user ID.
func GetOwnedOrgsByUserID(userID int64) ([]*Organization, error) {
	return getOwnedOrgsByUserID(db.GetEngine(db.DefaultContext), userID)
}

// GetOwnedOrgsByUserIDDesc returns a list of organizations are owned by
// given user ID, ordered descending by the given condition.
func GetOwnedOrgsByUserIDDesc(userID int64, desc string) ([]*Organization, error) {
	return getOwnedOrgsByUserID(db.GetEngine(db.DefaultContext).Desc(desc), userID)
}

// GetOrgsCanCreateRepoByUserID returns a list of organizations where given user ID
// are allowed to create repos.
func GetOrgsCanCreateRepoByUserID(userID int64) ([]*Organization, error) {
	orgs := make([]*Organization, 0, 10)

	return orgs, db.GetEngine(db.DefaultContext).Where(builder.In("id", builder.Select("`user`.id").From("`user`").
		Join("INNER", "`team_user`", "`team_user`.org_id = `user`.id").
		Join("INNER", "`team`", "`team`.id = `team_user`.team_id").
		Where(builder.Eq{"`team_user`.uid": userID}).
		And(builder.Eq{"`team`.authorize": perm.AccessModeOwner}.Or(builder.Eq{"`team`.can_create_org_repo": true})))).
		Asc("`user`.name").
		Find(&orgs)
}

// GetOrgUsersByUserID returns all organization-user relations by user ID.
func GetOrgUsersByUserID(uid int64, opts *SearchOrganizationsOptions) ([]*OrgUser, error) {
	ous := make([]*OrgUser, 0, 10)
	sess := db.GetEngine(db.DefaultContext).
		Join("LEFT", "`user`", "`org_user`.org_id=`user`.id").
		Where("`org_user`.uid=?", uid)
	if !opts.All {
		// Only show public organizations
		sess.And("is_public=?", true)
	}

	if opts.PageSize != 0 {
		sess = db.SetSessionPagination(sess, opts)
	}

	err := sess.
		Asc("`user`.name").
		Find(&ous)
	return ous, err
}

// GetOrgUsersByOrgID returns all organization-user relations by organization ID.
func GetOrgUsersByOrgID(opts *FindOrgMembersOpts) ([]*OrgUser, error) {
	return getOrgUsersByOrgID(db.GetEngine(db.DefaultContext), opts)
}

func getOrgUsersByOrgID(e db.Engine, opts *FindOrgMembersOpts) ([]*OrgUser, error) {
	sess := e.Where("org_id=?", opts.OrgID)
	if opts.PublicOnly {
		sess.And("is_public = ?", true)
	}
	if opts.ListOptions.PageSize > 0 {
		sess = db.SetSessionPagination(sess, opts)

		ous := make([]*OrgUser, 0, opts.PageSize)
		return ous, sess.Find(&ous)
	}

	var ous []*OrgUser
	return ous, sess.Find(&ous)
}

// ChangeOrgUserStatus changes public or private membership status.
func ChangeOrgUserStatus(orgID, uid int64, public bool) error {
	ou := new(OrgUser)
	has, err := db.GetEngine(db.DefaultContext).
		Where("uid=?", uid).
		And("org_id=?", orgID).
		Get(ou)
	if err != nil {
		return err
	} else if !has {
		return nil
	}

	ou.IsPublic = public
	_, err = db.GetEngine(db.DefaultContext).ID(ou.ID).Cols("is_public").Update(ou)
	return err
}

// AddOrgUser adds new user to given organization.
func AddOrgUser(orgID, uid int64) error {
	isAlreadyMember, err := IsOrganizationMember(orgID, uid)
	if err != nil || isAlreadyMember {
		return err
	}

	ctx, committer, err := db.TxContext()
	if err != nil {
		return err
	}
	defer committer.Close()

	ou := &OrgUser{
		UID:      uid,
		OrgID:    orgID,
		IsPublic: setting.Service.DefaultOrgMemberVisible,
	}

	if err := db.Insert(ctx, ou); err != nil {
		return err
	} else if _, err = db.Exec(ctx, "UPDATE `user` SET num_members = num_members + 1 WHERE id = ?", orgID); err != nil {
		return err
	}

	return committer.Commit()
}

// GetOrgByIDCtx returns the user object by given ID if exists.
func GetOrgByIDCtx(ctx context.Context, id int64) (*Organization, error) {
	u := new(Organization)
	has, err := db.GetEngine(ctx).ID(id).Get(u)
	if err != nil {
		return nil, err
	} else if !has {
		return nil, user_model.ErrUserNotExist{
			UID:   id,
			Name:  "",
			KeyID: 0,
		}
	}
	return u, nil
}

// GetOrgByID returns the user object by given ID if exists.
func GetOrgByID(id int64) (*Organization, error) {
	return GetOrgByIDCtx(db.DefaultContext, id)
}

func removeOrgUser(ctx context.Context, orgID, userID int64) error {
	ou := new(OrgUser)

	sess := db.GetEngine(ctx)

	has, err := sess.
		Where("uid=?", userID).
		And("org_id=?", orgID).
		Get(ou)
	if err != nil {
		return fmt.Errorf("get org-user: %v", err)
	} else if !has {
		return nil
	}

	org, err := GetOrgByIDCtx(ctx, orgID)
	if err != nil {
		return fmt.Errorf("GetUserByID [%d]: %v", orgID, err)
	}

	// Check if the user to delete is the last member in owner team.
	if isOwner, err := isOrganizationOwner(sess, orgID, userID); err != nil {
		return err
	} else if isOwner {
		t, err := org.getOwnerTeam(sess)
		if err != nil {
			return err
		}
		if t.NumMembers == 1 {
			if err := t.getMembers(sess); err != nil {
				return err
			}
			if t.Members[0].ID == userID {
				return ErrLastOrgOwner{UID: userID}
			}
		}
	}

	if _, err := sess.ID(ou.ID).Delete(ou); err != nil {
		return err
	} else if _, err = sess.Exec("UPDATE `user` SET num_members=num_members-1 WHERE id=?", orgID); err != nil {
		return err
	}

	// Delete all repository accesses and unwatch them.
	env, err := org.accessibleReposEnv(sess, userID)
	if err != nil {
		return fmt.Errorf("AccessibleReposEnv: %v", err)
	}
	repoIDs, err := env.RepoIDs(1, org.NumRepos)
	if err != nil {
		return fmt.Errorf("GetUserRepositories [%d]: %v", userID, err)
	}
	for _, repoID := range repoIDs {
		if err = repo_model.WatchRepoCtx(ctx, userID, repoID, false); err != nil {
			return err
		}
	}

	if len(repoIDs) > 0 {
		if _, err = sess.
			Where("user_id = ?", userID).
			In("repo_id", repoIDs).
			Delete(new(Access)); err != nil {
			return err
		}
	}

	// Delete member in his/her teams.
	teams, err := getUserOrgTeams(sess, org.ID, userID)
	if err != nil {
		return err
	}
	for _, t := range teams {
		if err = removeTeamMember(ctx, t, userID); err != nil {
			return err
		}
	}

	return nil
}

// RemoveOrgUser removes user from given organization.
func RemoveOrgUser(orgID, userID int64) error {
	ctx, committer, err := db.TxContext()
	if err != nil {
		return err
	}
	defer committer.Close()
	if err := removeOrgUser(ctx, orgID, userID); err != nil {
		return err
	}
	return committer.Commit()
}

func removeOrgRepo(e db.Engine, orgID, repoID int64) error {
	teamRepos := make([]*TeamRepo, 0, 10)
	if err := e.Find(&teamRepos, &TeamRepo{OrgID: orgID, RepoID: repoID}); err != nil {
		return err
	}

	if len(teamRepos) == 0 {
		return nil
	}

	if _, err := e.Delete(&TeamRepo{
		OrgID:  orgID,
		RepoID: repoID,
	}); err != nil {
		return err
	}

	teamIDs := make([]int64, len(teamRepos))
	for i, teamRepo := range teamRepos {
		teamIDs[i] = teamRepo.TeamID
	}

	_, err := e.Decr("num_repos").In("id", teamIDs).Update(new(Team))
	return err
}

func (org *Organization) getUserTeams(e db.Engine, userID int64, cols ...string) ([]*Team, error) {
	teams := make([]*Team, 0, org.NumTeams)
	return teams, e.
		Where("`team_user`.org_id = ?", org.ID).
		Join("INNER", "team_user", "`team_user`.team_id = team.id").
		Join("INNER", "`user`", "`user`.id=team_user.uid").
		And("`team_user`.uid = ?", userID).
		Asc("`user`.name").
		Cols(cols...).
		Find(&teams)
}

func (org *Organization) getUserTeamIDs(e db.Engine, userID int64) ([]int64, error) {
	teamIDs := make([]int64, 0, org.NumTeams)
	return teamIDs, e.
		Table("team").
		Cols("team.id").
		Where("`team_user`.org_id = ?", org.ID).
		Join("INNER", "team_user", "`team_user`.team_id = team.id").
		And("`team_user`.uid = ?", userID).
		Find(&teamIDs)
}

// TeamsWithAccessToRepo returns all teams that have given access level to the repository.
func (org *Organization) TeamsWithAccessToRepo(repoID int64, mode perm.AccessMode) ([]*Team, error) {
	return GetTeamsWithAccessToRepo(org.ID, repoID, mode)
}

// GetUserTeamIDs returns of all team IDs of the organization that user is member of.
func (org *Organization) GetUserTeamIDs(userID int64) ([]int64, error) {
	return org.getUserTeamIDs(db.GetEngine(db.DefaultContext), userID)
}

// GetUserTeams returns all teams that belong to user,
// and that the user has joined.
func (org *Organization) GetUserTeams(userID int64) ([]*Team, error) {
	return org.getUserTeams(db.GetEngine(db.DefaultContext), userID)
}

// AccessibleReposEnvironment operations involving the repositories that are
// accessible to a particular user
type AccessibleReposEnvironment interface {
	CountRepos() (int64, error)
	RepoIDs(page, pageSize int) ([]int64, error)
	Repos(page, pageSize int) ([]*repo_model.Repository, error)
	MirrorRepos() ([]*repo_model.Repository, error)
	AddKeyword(keyword string)
	SetSort(db.SearchOrderBy)
}

type accessibleReposEnv struct {
	org     *Organization
	user    *user_model.User
	team    *Team
	teamIDs []int64
	e       db.Engine
	keyword string
	orderBy db.SearchOrderBy
}

// AccessibleReposEnv builds an AccessibleReposEnvironment for the repositories in `org`
// that are accessible to the specified user.
func (org *Organization) AccessibleReposEnv(userID int64) (AccessibleReposEnvironment, error) {
	return org.accessibleReposEnv(db.GetEngine(db.DefaultContext), userID)
}

func (org *Organization) accessibleReposEnv(e db.Engine, userID int64) (AccessibleReposEnvironment, error) {
	var user *user_model.User

	if userID > 0 {
		u, err := user_model.GetUserByIDEngine(e, userID)
		if err != nil {
			return nil, err
		}
		user = u
	}

	teamIDs, err := org.getUserTeamIDs(e, userID)
	if err != nil {
		return nil, err
	}
	return &accessibleReposEnv{
		org:     org,
		user:    user,
		teamIDs: teamIDs,
		e:       e,
		orderBy: db.SearchOrderByRecentUpdated,
	}, nil
}

// AccessibleTeamReposEnv an AccessibleReposEnvironment for the repositories in `org`
// that are accessible to the specified team.
func (org *Organization) AccessibleTeamReposEnv(team *Team) AccessibleReposEnvironment {
	return &accessibleReposEnv{
		org:     org,
		team:    team,
		e:       db.GetEngine(db.DefaultContext),
		orderBy: db.SearchOrderByRecentUpdated,
	}
}

func (env *accessibleReposEnv) cond() builder.Cond {
	cond := builder.NewCond()
	if env.team != nil {
		cond = cond.And(builder.Eq{"team_repo.team_id": env.team.ID})
	} else {
		if env.user == nil || !env.user.IsRestricted {
			cond = cond.Or(builder.Eq{
				"`repository`.owner_id":   env.org.ID,
				"`repository`.is_private": false,
			})
		}
		if len(env.teamIDs) > 0 {
			cond = cond.Or(builder.In("team_repo.team_id", env.teamIDs))
		}
	}
	if env.keyword != "" {
		cond = cond.And(builder.Like{"`repository`.lower_name", strings.ToLower(env.keyword)})
	}
	return cond
}

func (env *accessibleReposEnv) CountRepos() (int64, error) {
	repoCount, err := env.e.
		Join("INNER", "team_repo", "`team_repo`.repo_id=`repository`.id").
		Where(env.cond()).
		Distinct("`repository`.id").
		Count(&repo_model.Repository{})
	if err != nil {
		return 0, fmt.Errorf("count user repositories in organization: %v", err)
	}
	return repoCount, nil
}

func (env *accessibleReposEnv) RepoIDs(page, pageSize int) ([]int64, error) {
	if page <= 0 {
		page = 1
	}

	repoIDs := make([]int64, 0, pageSize)
	return repoIDs, env.e.
		Table("repository").
		Join("INNER", "team_repo", "`team_repo`.repo_id=`repository`.id").
		Where(env.cond()).
		GroupBy("`repository`.id,`repository`."+strings.Fields(string(env.orderBy))[0]).
		OrderBy(string(env.orderBy)).
		Limit(pageSize, (page-1)*pageSize).
		Cols("`repository`.id").
		Find(&repoIDs)
}

func (env *accessibleReposEnv) Repos(page, pageSize int) ([]*repo_model.Repository, error) {
	repoIDs, err := env.RepoIDs(page, pageSize)
	if err != nil {
		return nil, fmt.Errorf("GetUserRepositoryIDs: %v", err)
	}

	repos := make([]*repo_model.Repository, 0, len(repoIDs))
	if len(repoIDs) == 0 {
		return repos, nil
	}

	return repos, env.e.
		In("`repository`.id", repoIDs).
		OrderBy(string(env.orderBy)).
		Find(&repos)
}

func (env *accessibleReposEnv) MirrorRepoIDs() ([]int64, error) {
	repoIDs := make([]int64, 0, 10)
	return repoIDs, env.e.
		Table("repository").
		Join("INNER", "team_repo", "`team_repo`.repo_id=`repository`.id AND `repository`.is_mirror=?", true).
		Where(env.cond()).
		GroupBy("`repository`.id, `repository`.updated_unix").
		OrderBy(string(env.orderBy)).
		Cols("`repository`.id").
		Find(&repoIDs)
}

func (env *accessibleReposEnv) MirrorRepos() ([]*repo_model.Repository, error) {
	repoIDs, err := env.MirrorRepoIDs()
	if err != nil {
		return nil, fmt.Errorf("MirrorRepoIDs: %v", err)
	}

	repos := make([]*repo_model.Repository, 0, len(repoIDs))
	if len(repoIDs) == 0 {
		return repos, nil
	}

	return repos, env.e.
		In("`repository`.id", repoIDs).
		Find(&repos)
}

func (env *accessibleReposEnv) AddKeyword(keyword string) {
	env.keyword = keyword
}

func (env *accessibleReposEnv) SetSort(orderBy db.SearchOrderBy) {
	env.orderBy = orderBy
}
