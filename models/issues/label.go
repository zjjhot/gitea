// Copyright 2016 The Gogs Authors. All rights reserved.
// Copyright 2020 The Gitea Authors.
// SPDX-License-Identifier: MIT

package issues

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"code.gitea.io/gitea/models/db"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/label"
	"code.gitea.io/gitea/modules/timeutil"
	"code.gitea.io/gitea/modules/util"

	"xorm.io/builder"
)

// ErrRepoLabelNotExist represents a "RepoLabelNotExist" kind of error.
type ErrRepoLabelNotExist struct {
	LabelID int64
	RepoID  int64
}

// IsErrRepoLabelNotExist checks if an error is a RepoErrLabelNotExist.
func IsErrRepoLabelNotExist(err error) bool {
	_, ok := err.(ErrRepoLabelNotExist)
	return ok
}

func (err ErrRepoLabelNotExist) Error() string {
	return fmt.Sprintf("label does not exist [label_id: %d, repo_id: %d]", err.LabelID, err.RepoID)
}

func (err ErrRepoLabelNotExist) Unwrap() error {
	return util.ErrNotExist
}

// ErrOrgLabelNotExist represents a "OrgLabelNotExist" kind of error.
type ErrOrgLabelNotExist struct {
	LabelID int64
	OrgID   int64
}

// IsErrOrgLabelNotExist checks if an error is a OrgErrLabelNotExist.
func IsErrOrgLabelNotExist(err error) bool {
	_, ok := err.(ErrOrgLabelNotExist)
	return ok
}

func (err ErrOrgLabelNotExist) Error() string {
	return fmt.Sprintf("label does not exist [label_id: %d, org_id: %d]", err.LabelID, err.OrgID)
}

func (err ErrOrgLabelNotExist) Unwrap() error {
	return util.ErrNotExist
}

// ErrLabelNotExist represents a "LabelNotExist" kind of error.
type ErrLabelNotExist struct {
	LabelID int64
}

// IsErrLabelNotExist checks if an error is a ErrLabelNotExist.
func IsErrLabelNotExist(err error) bool {
	_, ok := err.(ErrLabelNotExist)
	return ok
}

func (err ErrLabelNotExist) Error() string {
	return fmt.Sprintf("label does not exist [label_id: %d]", err.LabelID)
}

func (err ErrLabelNotExist) Unwrap() error {
	return util.ErrNotExist
}

// Label represents a label of repository for issues.
type Label struct {
	ID              int64 `xorm:"pk autoincr"`
	RepoID          int64 `xorm:"INDEX"`
	OrgID           int64 `xorm:"INDEX"`
	Name            string
	Exclusive       bool
	Description     string
	Color           string `xorm:"VARCHAR(7)"`
	NumIssues       int
	NumClosedIssues int
	CreatedUnix     timeutil.TimeStamp `xorm:"INDEX created"`
	UpdatedUnix     timeutil.TimeStamp `xorm:"INDEX updated"`

	NumOpenIssues     int    `xorm:"-"`
	NumOpenRepoIssues int64  `xorm:"-"`
	IsChecked         bool   `xorm:"-"`
	QueryString       string `xorm:"-"`
	IsSelected        bool   `xorm:"-"`
	IsExcluded        bool   `xorm:"-"`
}

func init() {
	db.RegisterModel(new(Label))
	db.RegisterModel(new(IssueLabel))
}

// CalOpenIssues sets the number of open issues of a label based on the already stored number of closed issues.
func (l *Label) CalOpenIssues() {
	l.NumOpenIssues = l.NumIssues - l.NumClosedIssues
}

// CalOpenOrgIssues calculates the open issues of a label for a specific repo
func (l *Label) CalOpenOrgIssues(ctx context.Context, repoID, labelID int64) {
	counts, _ := CountIssuesByRepo(ctx, &IssuesOptions{
		RepoID:   repoID,
		LabelIDs: []int64{labelID},
		IsClosed: util.OptionalBoolFalse,
	})

	for _, count := range counts {
		l.NumOpenRepoIssues += count
	}
}

// LoadSelectedLabelsAfterClick calculates the set of selected labels when a label is clicked
func (l *Label) LoadSelectedLabelsAfterClick(currentSelectedLabels []int64, currentSelectedExclusiveScopes []string) {
	var labelQuerySlice []string
	labelSelected := false
	labelID := strconv.FormatInt(l.ID, 10)
	labelScope := l.ExclusiveScope()
	for i, s := range currentSelectedLabels {
		if s == l.ID {
			labelSelected = true
		} else if -s == l.ID {
			labelSelected = true
			l.IsExcluded = true
		} else if s != 0 {
			// Exclude other labels in the same scope from selection
			if s < 0 || labelScope == "" || labelScope != currentSelectedExclusiveScopes[i] {
				labelQuerySlice = append(labelQuerySlice, strconv.FormatInt(s, 10))
			}
		}
	}
	if !labelSelected {
		labelQuerySlice = append(labelQuerySlice, labelID)
	}
	l.IsSelected = labelSelected
	l.QueryString = strings.Join(labelQuerySlice, ",")
}

// BelongsToOrg returns true if label is an organization label
func (l *Label) BelongsToOrg() bool {
	return l.OrgID > 0
}

// BelongsToRepo returns true if label is a repository label
func (l *Label) BelongsToRepo() bool {
	return l.RepoID > 0
}

// Get color as RGB values in 0..255 range
func (l *Label) ColorRGB() (float64, float64, float64, error) {
	color, err := strconv.ParseUint(l.Color[1:], 16, 64)
	if err != nil {
		return 0, 0, 0, err
	}

	r := float64(uint8(0xFF & (uint32(color) >> 16)))
	g := float64(uint8(0xFF & (uint32(color) >> 8)))
	b := float64(uint8(0xFF & uint32(color)))
	return r, g, b, nil
}

// Determine if label text should be light or dark to be readable on background color
func (l *Label) UseLightTextColor() bool {
	if strings.HasPrefix(l.Color, "#") {
		if r, g, b, err := l.ColorRGB(); err == nil {
			// Perceived brightness from: https://www.w3.org/TR/AERT/#color-contrast
			// In the future WCAG 3 APCA may be a better solution
			brightness := (0.299*r + 0.587*g + 0.114*b) / 255
			return brightness < 0.35
		}
	}

	return false
}

// Return scope substring of label name, or empty string if none exists
func (l *Label) ExclusiveScope() string {
	if !l.Exclusive {
		return ""
	}
	lastIndex := strings.LastIndex(l.Name, "/")
	if lastIndex == -1 || lastIndex == 0 || lastIndex == len(l.Name)-1 {
		return ""
	}
	return l.Name[:lastIndex]
}

// NewLabel creates a new label
func NewLabel(ctx context.Context, l *Label) error {
	color, err := label.NormalizeColor(l.Color)
	if err != nil {
		return err
	}
	l.Color = color

	return db.Insert(ctx, l)
}

// NewLabels creates new labels
func NewLabels(labels ...*Label) error {
	ctx, committer, err := db.TxContext(db.DefaultContext)
	if err != nil {
		return err
	}
	defer committer.Close()

	for _, l := range labels {
		color, err := label.NormalizeColor(l.Color)
		if err != nil {
			return err
		}
		l.Color = color

		if err := db.Insert(ctx, l); err != nil {
			return err
		}
	}
	return committer.Commit()
}

// UpdateLabel updates label information.
func UpdateLabel(l *Label) error {
	color, err := label.NormalizeColor(l.Color)
	if err != nil {
		return err
	}
	l.Color = color

	return updateLabelCols(db.DefaultContext, l, "name", "description", "color", "exclusive")
}

// DeleteLabel delete a label
func DeleteLabel(id, labelID int64) error {
	l, err := GetLabelByID(db.DefaultContext, labelID)
	if err != nil {
		if IsErrLabelNotExist(err) {
			return nil
		}
		return err
	}

	ctx, committer, err := db.TxContext(db.DefaultContext)
	if err != nil {
		return err
	}
	defer committer.Close()

	sess := db.GetEngine(ctx)

	if l.BelongsToOrg() && l.OrgID != id {
		return nil
	}
	if l.BelongsToRepo() && l.RepoID != id {
		return nil
	}

	if _, err = sess.ID(labelID).Delete(new(Label)); err != nil {
		return err
	} else if _, err = sess.
		Where("label_id = ?", labelID).
		Delete(new(IssueLabel)); err != nil {
		return err
	}

	// delete comments about now deleted label_id
	if _, err = sess.Where("label_id = ?", labelID).Cols("label_id").Delete(&Comment{}); err != nil {
		return err
	}

	return committer.Commit()
}

// GetLabelByID returns a label by given ID.
func GetLabelByID(ctx context.Context, labelID int64) (*Label, error) {
	if labelID <= 0 {
		return nil, ErrLabelNotExist{labelID}
	}

	l := &Label{}
	has, err := db.GetEngine(ctx).ID(labelID).Get(l)
	if err != nil {
		return nil, err
	} else if !has {
		return nil, ErrLabelNotExist{l.ID}
	}
	return l, nil
}

// GetLabelsByIDs returns a list of labels by IDs
func GetLabelsByIDs(labelIDs []int64) ([]*Label, error) {
	labels := make([]*Label, 0, len(labelIDs))
	return labels, db.GetEngine(db.DefaultContext).Table("label").
		In("id", labelIDs).
		Asc("name").
		Cols("id", "repo_id", "org_id").
		Find(&labels)
}

// __________                           .__  __
// \______   \ ____ ______   ____  _____|__|/  |_  ___________ ___.__.
//  |       _// __ \\____ \ /  _ \/  ___/  \   __\/  _ \_  __ <   |  |
//  |    |   \  ___/|  |_> >  <_> )___ \|  ||  | (  <_> )  | \/\___  |
//  |____|_  /\___  >   __/ \____/____  >__||__|  \____/|__|   / ____|
//         \/     \/|__|              \/                       \/

// GetLabelInRepoByName returns a label by name in given repository.
func GetLabelInRepoByName(ctx context.Context, repoID int64, labelName string) (*Label, error) {
	if len(labelName) == 0 || repoID <= 0 {
		return nil, ErrRepoLabelNotExist{0, repoID}
	}

	l := &Label{
		Name:   labelName,
		RepoID: repoID,
	}
	has, err := db.GetByBean(ctx, l)
	if err != nil {
		return nil, err
	} else if !has {
		return nil, ErrRepoLabelNotExist{0, l.RepoID}
	}
	return l, nil
}

// GetLabelInRepoByID returns a label by ID in given repository.
func GetLabelInRepoByID(ctx context.Context, repoID, labelID int64) (*Label, error) {
	if labelID <= 0 || repoID <= 0 {
		return nil, ErrRepoLabelNotExist{labelID, repoID}
	}

	l := &Label{
		ID:     labelID,
		RepoID: repoID,
	}
	has, err := db.GetByBean(ctx, l)
	if err != nil {
		return nil, err
	} else if !has {
		return nil, ErrRepoLabelNotExist{l.ID, l.RepoID}
	}
	return l, nil
}

// GetLabelIDsInRepoByNames returns a list of labelIDs by names in a given
// repository.
// it silently ignores label names that do not belong to the repository.
func GetLabelIDsInRepoByNames(repoID int64, labelNames []string) ([]int64, error) {
	labelIDs := make([]int64, 0, len(labelNames))
	return labelIDs, db.GetEngine(db.DefaultContext).Table("label").
		Where("repo_id = ?", repoID).
		In("name", labelNames).
		Asc("name").
		Cols("id").
		Find(&labelIDs)
}

// BuildLabelNamesIssueIDsCondition returns a builder where get issue ids match label names
func BuildLabelNamesIssueIDsCondition(labelNames []string) *builder.Builder {
	return builder.Select("issue_label.issue_id").
		From("issue_label").
		InnerJoin("label", "label.id = issue_label.label_id").
		Where(
			builder.In("label.name", labelNames),
		).
		GroupBy("issue_label.issue_id")
}

// GetLabelsInRepoByIDs returns a list of labels by IDs in given repository,
// it silently ignores label IDs that do not belong to the repository.
func GetLabelsInRepoByIDs(ctx context.Context, repoID int64, labelIDs []int64) ([]*Label, error) {
	labels := make([]*Label, 0, len(labelIDs))
	return labels, db.GetEngine(ctx).
		Where("repo_id = ?", repoID).
		In("id", labelIDs).
		Asc("name").
		Find(&labels)
}

// GetLabelsByRepoID returns all labels that belong to given repository by ID.
func GetLabelsByRepoID(ctx context.Context, repoID int64, sortType string, listOptions db.ListOptions) ([]*Label, error) {
	if repoID <= 0 {
		return nil, ErrRepoLabelNotExist{0, repoID}
	}
	labels := make([]*Label, 0, 10)
	sess := db.GetEngine(ctx).Where("repo_id = ?", repoID)

	switch sortType {
	case "reversealphabetically":
		sess.Desc("name")
	case "leastissues":
		sess.Asc("num_issues")
	case "mostissues":
		sess.Desc("num_issues")
	default:
		sess.Asc("name")
	}

	if listOptions.Page != 0 {
		sess = db.SetSessionPagination(sess, &listOptions)
	}

	return labels, sess.Find(&labels)
}

// CountLabelsByRepoID count number of all labels that belong to given repository by ID.
func CountLabelsByRepoID(repoID int64) (int64, error) {
	return db.GetEngine(db.DefaultContext).Where("repo_id = ?", repoID).Count(&Label{})
}

// ________
// \_____  \_______  ____
//  /   |   \_  __ \/ ___\
// /    |    \  | \/ /_/  >
// \_______  /__|  \___  /
//         \/     /_____/

// GetLabelInOrgByName returns a label by name in given organization.
func GetLabelInOrgByName(ctx context.Context, orgID int64, labelName string) (*Label, error) {
	if len(labelName) == 0 || orgID <= 0 {
		return nil, ErrOrgLabelNotExist{0, orgID}
	}

	l := &Label{
		Name:  labelName,
		OrgID: orgID,
	}
	has, err := db.GetByBean(ctx, l)
	if err != nil {
		return nil, err
	} else if !has {
		return nil, ErrOrgLabelNotExist{0, l.OrgID}
	}
	return l, nil
}

// GetLabelInOrgByID returns a label by ID in given organization.
func GetLabelInOrgByID(ctx context.Context, orgID, labelID int64) (*Label, error) {
	if labelID <= 0 || orgID <= 0 {
		return nil, ErrOrgLabelNotExist{labelID, orgID}
	}

	l := &Label{
		ID:    labelID,
		OrgID: orgID,
	}
	has, err := db.GetByBean(ctx, l)
	if err != nil {
		return nil, err
	} else if !has {
		return nil, ErrOrgLabelNotExist{l.ID, l.OrgID}
	}
	return l, nil
}

// GetLabelIDsInOrgByNames returns a list of labelIDs by names in a given
// organization.
func GetLabelIDsInOrgByNames(orgID int64, labelNames []string) ([]int64, error) {
	if orgID <= 0 {
		return nil, ErrOrgLabelNotExist{0, orgID}
	}
	labelIDs := make([]int64, 0, len(labelNames))

	return labelIDs, db.GetEngine(db.DefaultContext).Table("label").
		Where("org_id = ?", orgID).
		In("name", labelNames).
		Asc("name").
		Cols("id").
		Find(&labelIDs)
}

// GetLabelsInOrgByIDs returns a list of labels by IDs in given organization,
// it silently ignores label IDs that do not belong to the organization.
func GetLabelsInOrgByIDs(ctx context.Context, orgID int64, labelIDs []int64) ([]*Label, error) {
	labels := make([]*Label, 0, len(labelIDs))
	return labels, db.GetEngine(ctx).
		Where("org_id = ?", orgID).
		In("id", labelIDs).
		Asc("name").
		Find(&labels)
}

// GetLabelsByOrgID returns all labels that belong to given organization by ID.
func GetLabelsByOrgID(ctx context.Context, orgID int64, sortType string, listOptions db.ListOptions) ([]*Label, error) {
	if orgID <= 0 {
		return nil, ErrOrgLabelNotExist{0, orgID}
	}
	labels := make([]*Label, 0, 10)
	sess := db.GetEngine(ctx).Where("org_id = ?", orgID)

	switch sortType {
	case "reversealphabetically":
		sess.Desc("name")
	case "leastissues":
		sess.Asc("num_issues")
	case "mostissues":
		sess.Desc("num_issues")
	default:
		sess.Asc("name")
	}

	if listOptions.Page != 0 {
		sess = db.SetSessionPagination(sess, &listOptions)
	}

	return labels, sess.Find(&labels)
}

// CountLabelsByOrgID count all labels that belong to given organization by ID.
func CountLabelsByOrgID(orgID int64) (int64, error) {
	return db.GetEngine(db.DefaultContext).Where("org_id = ?", orgID).Count(&Label{})
}

// .___
// |   | ______ ________ __   ____
// |   |/  ___//  ___/  |  \_/ __ \
// |   |\___ \ \___ \|  |  /\  ___/
// |___/____  >____  >____/  \___ |
//          \/     \/            \/

// GetLabelsByIssueID returns all labels that belong to given issue by ID.
func GetLabelsByIssueID(ctx context.Context, issueID int64) ([]*Label, error) {
	var labels []*Label
	return labels, db.GetEngine(ctx).Where("issue_label.issue_id = ?", issueID).
		Join("LEFT", "issue_label", "issue_label.label_id = label.id").
		Asc("label.name").
		Find(&labels)
}

func updateLabelCols(ctx context.Context, l *Label, cols ...string) error {
	_, err := db.GetEngine(ctx).ID(l.ID).
		SetExpr("num_issues",
			builder.Select("count(*)").From("issue_label").
				Where(builder.Eq{"label_id": l.ID}),
		).
		SetExpr("num_closed_issues",
			builder.Select("count(*)").From("issue_label").
				InnerJoin("issue", "issue_label.issue_id = issue.id").
				Where(builder.Eq{
					"issue_label.label_id": l.ID,
					"issue.is_closed":      true,
				}),
		).
		Cols(cols...).Update(l)
	return err
}

// .___                            .____          ___.          .__
// |   | ______ ________ __   ____ |    |   _____ \_ |__   ____ |  |
// |   |/  ___//  ___/  |  \_/ __ \|    |   \__  \ | __ \_/ __ \|  |
// |   |\___ \ \___ \|  |  /\  ___/|    |___ / __ \| \_\ \  ___/|  |__
// |___/____  >____  >____/  \___  >_______ (____  /___  /\___  >____/
//          \/     \/            \/        \/    \/    \/     \/

// IssueLabel represents an issue-label relation.
type IssueLabel struct {
	ID      int64 `xorm:"pk autoincr"`
	IssueID int64 `xorm:"UNIQUE(s)"`
	LabelID int64 `xorm:"UNIQUE(s)"`
}

// HasIssueLabel returns true if issue has been labeled.
func HasIssueLabel(ctx context.Context, issueID, labelID int64) bool {
	has, _ := db.GetEngine(ctx).Where("issue_id = ? AND label_id = ?", issueID, labelID).Get(new(IssueLabel))
	return has
}

// newIssueLabel this function creates a new label it does not check if the label is valid for the issue
// YOU MUST CHECK THIS BEFORE THIS FUNCTION
func newIssueLabel(ctx context.Context, issue *Issue, label *Label, doer *user_model.User) (err error) {
	if err = db.Insert(ctx, &IssueLabel{
		IssueID: issue.ID,
		LabelID: label.ID,
	}); err != nil {
		return err
	}

	if err = issue.LoadRepo(ctx); err != nil {
		return
	}

	opts := &CreateCommentOptions{
		Type:    CommentTypeLabel,
		Doer:    doer,
		Repo:    issue.Repo,
		Issue:   issue,
		Label:   label,
		Content: "1",
	}
	if _, err = CreateComment(ctx, opts); err != nil {
		return err
	}

	return updateLabelCols(ctx, label, "num_issues", "num_closed_issue")
}

// Remove all issue labels in the given exclusive scope
func RemoveDuplicateExclusiveIssueLabels(ctx context.Context, issue *Issue, label *Label, doer *user_model.User) (err error) {
	scope := label.ExclusiveScope()
	if scope == "" {
		return nil
	}

	var toRemove []*Label
	for _, issueLabel := range issue.Labels {
		if label.ID != issueLabel.ID && issueLabel.ExclusiveScope() == scope {
			toRemove = append(toRemove, issueLabel)
		}
	}

	for _, issueLabel := range toRemove {
		if err = deleteIssueLabel(ctx, issue, issueLabel, doer); err != nil {
			return err
		}
	}

	return nil
}

// NewIssueLabel creates a new issue-label relation.
func NewIssueLabel(issue *Issue, label *Label, doer *user_model.User) (err error) {
	if HasIssueLabel(db.DefaultContext, issue.ID, label.ID) {
		return nil
	}

	ctx, committer, err := db.TxContext(db.DefaultContext)
	if err != nil {
		return err
	}
	defer committer.Close()

	if err = issue.LoadRepo(ctx); err != nil {
		return err
	}

	// Do NOT add invalid labels
	if issue.RepoID != label.RepoID && issue.Repo.OwnerID != label.OrgID {
		return nil
	}

	if err = RemoveDuplicateExclusiveIssueLabels(ctx, issue, label, doer); err != nil {
		return nil
	}

	if err = newIssueLabel(ctx, issue, label, doer); err != nil {
		return err
	}

	issue.Labels = nil
	if err = issue.LoadLabels(ctx); err != nil {
		return err
	}

	return committer.Commit()
}

// newIssueLabels add labels to an issue. It will check if the labels are valid for the issue
func newIssueLabels(ctx context.Context, issue *Issue, labels []*Label, doer *user_model.User) (err error) {
	if err = issue.LoadRepo(ctx); err != nil {
		return err
	}
	for _, l := range labels {
		// Don't add already present labels and invalid labels
		if HasIssueLabel(ctx, issue.ID, l.ID) ||
			(l.RepoID != issue.RepoID && l.OrgID != issue.Repo.OwnerID) {
			continue
		}

		if err = newIssueLabel(ctx, issue, l, doer); err != nil {
			return fmt.Errorf("newIssueLabel: %w", err)
		}
	}

	return nil
}

// NewIssueLabels creates a list of issue-label relations.
func NewIssueLabels(issue *Issue, labels []*Label, doer *user_model.User) (err error) {
	ctx, committer, err := db.TxContext(db.DefaultContext)
	if err != nil {
		return err
	}
	defer committer.Close()

	if err = newIssueLabels(ctx, issue, labels, doer); err != nil {
		return err
	}

	issue.Labels = nil
	if err = issue.LoadLabels(ctx); err != nil {
		return err
	}

	return committer.Commit()
}

func deleteIssueLabel(ctx context.Context, issue *Issue, label *Label, doer *user_model.User) (err error) {
	if count, err := db.DeleteByBean(ctx, &IssueLabel{
		IssueID: issue.ID,
		LabelID: label.ID,
	}); err != nil {
		return err
	} else if count == 0 {
		return nil
	}

	if err = issue.LoadRepo(ctx); err != nil {
		return
	}

	opts := &CreateCommentOptions{
		Type:  CommentTypeLabel,
		Doer:  doer,
		Repo:  issue.Repo,
		Issue: issue,
		Label: label,
	}
	if _, err = CreateComment(ctx, opts); err != nil {
		return err
	}

	return updateLabelCols(ctx, label, "num_issues", "num_closed_issue")
}

// DeleteIssueLabel deletes issue-label relation.
func DeleteIssueLabel(ctx context.Context, issue *Issue, label *Label, doer *user_model.User) error {
	if err := deleteIssueLabel(ctx, issue, label, doer); err != nil {
		return err
	}

	issue.Labels = nil
	return issue.LoadLabels(ctx)
}

// DeleteLabelsByRepoID  deletes labels of some repository
func DeleteLabelsByRepoID(ctx context.Context, repoID int64) error {
	deleteCond := builder.Select("id").From("label").Where(builder.Eq{"label.repo_id": repoID})

	if _, err := db.GetEngine(ctx).In("label_id", deleteCond).
		Delete(&IssueLabel{}); err != nil {
		return err
	}

	_, err := db.DeleteByBean(ctx, &Label{RepoID: repoID})
	return err
}

// CountOrphanedLabels return count of labels witch are broken and not accessible via ui anymore
func CountOrphanedLabels(ctx context.Context) (int64, error) {
	noref, err := db.GetEngine(ctx).Table("label").Where("repo_id=? AND org_id=?", 0, 0).Count()
	if err != nil {
		return 0, err
	}

	norepo, err := db.GetEngine(ctx).Table("label").
		Where(builder.And(
			builder.Gt{"repo_id": 0},
			builder.NotIn("repo_id", builder.Select("id").From("`repository`")),
		)).
		Count()
	if err != nil {
		return 0, err
	}

	noorg, err := db.GetEngine(ctx).Table("label").
		Where(builder.And(
			builder.Gt{"org_id": 0},
			builder.NotIn("org_id", builder.Select("id").From("`user`")),
		)).
		Count()
	if err != nil {
		return 0, err
	}

	return noref + norepo + noorg, nil
}

// DeleteOrphanedLabels delete labels witch are broken and not accessible via ui anymore
func DeleteOrphanedLabels(ctx context.Context) error {
	// delete labels with no reference
	if _, err := db.GetEngine(ctx).Table("label").Where("repo_id=? AND org_id=?", 0, 0).Delete(new(Label)); err != nil {
		return err
	}

	// delete labels with none existing repos
	if _, err := db.GetEngine(ctx).
		Where(builder.And(
			builder.Gt{"repo_id": 0},
			builder.NotIn("repo_id", builder.Select("id").From("`repository`")),
		)).
		Delete(Label{}); err != nil {
		return err
	}

	// delete labels with none existing orgs
	if _, err := db.GetEngine(ctx).
		Where(builder.And(
			builder.Gt{"org_id": 0},
			builder.NotIn("org_id", builder.Select("id").From("`user`")),
		)).
		Delete(Label{}); err != nil {
		return err
	}

	return nil
}

// CountOrphanedIssueLabels return count of IssueLabels witch have no label behind anymore
func CountOrphanedIssueLabels(ctx context.Context) (int64, error) {
	return db.GetEngine(ctx).Table("issue_label").
		NotIn("label_id", builder.Select("id").From("label")).
		Count()
}

// DeleteOrphanedIssueLabels delete IssueLabels witch have no label behind anymore
func DeleteOrphanedIssueLabels(ctx context.Context) error {
	_, err := db.GetEngine(ctx).
		NotIn("label_id", builder.Select("id").From("label")).
		Delete(IssueLabel{})
	return err
}

// CountIssueLabelWithOutsideLabels count label comments with outside label
func CountIssueLabelWithOutsideLabels(ctx context.Context) (int64, error) {
	return db.GetEngine(ctx).Where(builder.Expr("(label.org_id = 0 AND issue.repo_id != label.repo_id) OR (label.repo_id = 0 AND label.org_id != repository.owner_id)")).
		Table("issue_label").
		Join("inner", "label", "issue_label.label_id = label.id ").
		Join("inner", "issue", "issue.id = issue_label.issue_id ").
		Join("inner", "repository", "issue.repo_id = repository.id").
		Count(new(IssueLabel))
}

// FixIssueLabelWithOutsideLabels fix label comments with outside label
func FixIssueLabelWithOutsideLabels(ctx context.Context) (int64, error) {
	res, err := db.GetEngine(ctx).Exec(`DELETE FROM issue_label WHERE issue_label.id IN (
		SELECT il_too.id FROM (
			SELECT il_too_too.id
				FROM issue_label AS il_too_too
					INNER JOIN label ON il_too_too.label_id = label.id
					INNER JOIN issue on issue.id = il_too_too.issue_id
					INNER JOIN repository on repository.id = issue.repo_id
				WHERE
					(label.org_id = 0 AND issue.repo_id != label.repo_id) OR (label.repo_id = 0 AND label.org_id != repository.owner_id)
	) AS il_too )`)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}
