// Copyright 2014 The Gogs Authors. All rights reserved.
// Copyright 2017 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package webhook

import (
	"context"
	"fmt"
	"strings"

	"code.gitea.io/gitea/models/db"
	"code.gitea.io/gitea/modules/json"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/secret"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/modules/timeutil"
	"code.gitea.io/gitea/modules/util"

	"xorm.io/builder"
)

// ErrWebhookNotExist represents a "WebhookNotExist" kind of error.
type ErrWebhookNotExist struct {
	ID int64
}

// IsErrWebhookNotExist checks if an error is a ErrWebhookNotExist.
func IsErrWebhookNotExist(err error) bool {
	_, ok := err.(ErrWebhookNotExist)
	return ok
}

func (err ErrWebhookNotExist) Error() string {
	return fmt.Sprintf("webhook does not exist [id: %d]", err.ID)
}

func (err ErrWebhookNotExist) Unwrap() error {
	return util.ErrNotExist
}

// ErrHookTaskNotExist represents a "HookTaskNotExist" kind of error.
type ErrHookTaskNotExist struct {
	TaskID int64
	HookID int64
	UUID   string
}

// IsErrWebhookNotExist checks if an error is a ErrWebhookNotExist.
func IsErrHookTaskNotExist(err error) bool {
	_, ok := err.(ErrHookTaskNotExist)
	return ok
}

func (err ErrHookTaskNotExist) Error() string {
	return fmt.Sprintf("hook task does not exist [task: %d, hook: %d, uuid: %s]", err.TaskID, err.HookID, err.UUID)
}

func (err ErrHookTaskNotExist) Unwrap() error {
	return util.ErrNotExist
}

// HookContentType is the content type of a web hook
type HookContentType int

const (
	// ContentTypeJSON is a JSON payload for web hooks
	ContentTypeJSON HookContentType = iota + 1
	// ContentTypeForm is an url-encoded form payload for web hook
	ContentTypeForm
)

var hookContentTypes = map[string]HookContentType{
	"json": ContentTypeJSON,
	"form": ContentTypeForm,
}

// ToHookContentType returns HookContentType by given name.
func ToHookContentType(name string) HookContentType {
	return hookContentTypes[name]
}

// HookTaskCleanupType is the type of cleanup to perform on hook_task
type HookTaskCleanupType int

const (
	// OlderThan hook_task rows will be cleaned up by the age of the row
	OlderThan HookTaskCleanupType = iota
	// PerWebhook hook_task rows will be cleaned up by leaving the most recent deliveries for each webhook
	PerWebhook
)

var hookTaskCleanupTypes = map[string]HookTaskCleanupType{
	"OlderThan":  OlderThan,
	"PerWebhook": PerWebhook,
}

// ToHookTaskCleanupType returns HookTaskCleanupType by given name.
func ToHookTaskCleanupType(name string) HookTaskCleanupType {
	return hookTaskCleanupTypes[name]
}

// Name returns the name of a given web hook's content type
func (t HookContentType) Name() string {
	switch t {
	case ContentTypeJSON:
		return "json"
	case ContentTypeForm:
		return "form"
	}
	return ""
}

// IsValidHookContentType returns true if given name is a valid hook content type.
func IsValidHookContentType(name string) bool {
	_, ok := hookContentTypes[name]
	return ok
}

// HookEvents is a set of web hook events
type HookEvents struct {
	Create               bool `json:"create"`
	Delete               bool `json:"delete"`
	Fork                 bool `json:"fork"`
	Issues               bool `json:"issues"`
	IssueAssign          bool `json:"issue_assign"`
	IssueLabel           bool `json:"issue_label"`
	IssueMilestone       bool `json:"issue_milestone"`
	IssueComment         bool `json:"issue_comment"`
	Push                 bool `json:"push"`
	PullRequest          bool `json:"pull_request"`
	PullRequestAssign    bool `json:"pull_request_assign"`
	PullRequestLabel     bool `json:"pull_request_label"`
	PullRequestMilestone bool `json:"pull_request_milestone"`
	PullRequestComment   bool `json:"pull_request_comment"`
	PullRequestReview    bool `json:"pull_request_review"`
	PullRequestSync      bool `json:"pull_request_sync"`
	Wiki                 bool `json:"wiki"`
	Repository           bool `json:"repository"`
	Release              bool `json:"release"`
	Package              bool `json:"package"`
}

// HookEvent represents events that will delivery hook.
type HookEvent struct {
	PushOnly       bool   `json:"push_only"`
	SendEverything bool   `json:"send_everything"`
	ChooseEvents   bool   `json:"choose_events"`
	BranchFilter   string `json:"branch_filter"`

	HookEvents `json:"events"`
}

// HookType is the type of a webhook
type HookType = string

// Types of webhooks
const (
	GITEA      HookType = "gitea"
	GOGS       HookType = "gogs"
	SLACK      HookType = "slack"
	DISCORD    HookType = "discord"
	DINGTALK   HookType = "dingtalk"
	TELEGRAM   HookType = "telegram"
	MSTEAMS    HookType = "msteams"
	FEISHU     HookType = "feishu"
	MATRIX     HookType = "matrix"
	WECHATWORK HookType = "wechatwork"
	PACKAGIST  HookType = "packagist"
)

// HookStatus is the status of a web hook
type HookStatus int

// Possible statuses of a web hook
const (
	HookStatusNone = iota
	HookStatusSucceed
	HookStatusFail
)

// Webhook represents a web hook object.
type Webhook struct {
	ID              int64 `xorm:"pk autoincr"`
	RepoID          int64 `xorm:"INDEX"` // An ID of 0 indicates either a default or system webhook
	OrgID           int64 `xorm:"INDEX"`
	IsSystemWebhook bool
	URL             string `xorm:"url TEXT"`
	HTTPMethod      string `xorm:"http_method"`
	ContentType     HookContentType
	Secret          string `xorm:"TEXT"`
	Events          string `xorm:"TEXT"`
	*HookEvent      `xorm:"-"`
	IsActive        bool       `xorm:"INDEX"`
	Type            HookType   `xorm:"VARCHAR(16) 'type'"`
	Meta            string     `xorm:"TEXT"` // store hook-specific attributes
	LastStatus      HookStatus // Last delivery status

	// HeaderAuthorizationEncrypted should be accessed using HeaderAuthorization() and SetHeaderAuthorization()
	HeaderAuthorizationEncrypted string `xorm:"TEXT"`

	CreatedUnix timeutil.TimeStamp `xorm:"INDEX created"`
	UpdatedUnix timeutil.TimeStamp `xorm:"INDEX updated"`
}

func init() {
	db.RegisterModel(new(Webhook))
}

// AfterLoad updates the webhook object upon setting a column
func (w *Webhook) AfterLoad() {
	w.HookEvent = &HookEvent{}
	if err := json.Unmarshal([]byte(w.Events), w.HookEvent); err != nil {
		log.Error("Unmarshal[%d]: %v", w.ID, err)
	}
}

// History returns history of webhook by given conditions.
func (w *Webhook) History(page int) ([]*HookTask, error) {
	return HookTasks(w.ID, page)
}

// UpdateEvent handles conversion from HookEvent to Events.
func (w *Webhook) UpdateEvent() error {
	data, err := json.Marshal(w.HookEvent)
	w.Events = string(data)
	return err
}

// HasCreateEvent returns true if hook enabled create event.
func (w *Webhook) HasCreateEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.Create)
}

// HasDeleteEvent returns true if hook enabled delete event.
func (w *Webhook) HasDeleteEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.Delete)
}

// HasForkEvent returns true if hook enabled fork event.
func (w *Webhook) HasForkEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.Fork)
}

// HasIssuesEvent returns true if hook enabled issues event.
func (w *Webhook) HasIssuesEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.Issues)
}

// HasIssuesAssignEvent returns true if hook enabled issues assign event.
func (w *Webhook) HasIssuesAssignEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.IssueAssign)
}

// HasIssuesLabelEvent returns true if hook enabled issues label event.
func (w *Webhook) HasIssuesLabelEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.IssueLabel)
}

// HasIssuesMilestoneEvent returns true if hook enabled issues milestone event.
func (w *Webhook) HasIssuesMilestoneEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.IssueMilestone)
}

// HasIssueCommentEvent returns true if hook enabled issue_comment event.
func (w *Webhook) HasIssueCommentEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.IssueComment)
}

// HasPushEvent returns true if hook enabled push event.
func (w *Webhook) HasPushEvent() bool {
	return w.PushOnly || w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.Push)
}

// HasPullRequestEvent returns true if hook enabled pull request event.
func (w *Webhook) HasPullRequestEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.PullRequest)
}

// HasPullRequestAssignEvent returns true if hook enabled pull request assign event.
func (w *Webhook) HasPullRequestAssignEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.PullRequestAssign)
}

// HasPullRequestLabelEvent returns true if hook enabled pull request label event.
func (w *Webhook) HasPullRequestLabelEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.PullRequestLabel)
}

// HasPullRequestMilestoneEvent returns true if hook enabled pull request milestone event.
func (w *Webhook) HasPullRequestMilestoneEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.PullRequestMilestone)
}

// HasPullRequestCommentEvent returns true if hook enabled pull_request_comment event.
func (w *Webhook) HasPullRequestCommentEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.PullRequestComment)
}

// HasPullRequestApprovedEvent returns true if hook enabled pull request review event.
func (w *Webhook) HasPullRequestApprovedEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.PullRequestReview)
}

// HasPullRequestRejectedEvent returns true if hook enabled pull request review event.
func (w *Webhook) HasPullRequestRejectedEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.PullRequestReview)
}

// HasPullRequestReviewCommentEvent returns true if hook enabled pull request review event.
func (w *Webhook) HasPullRequestReviewCommentEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.PullRequestReview)
}

// HasPullRequestSyncEvent returns true if hook enabled pull request sync event.
func (w *Webhook) HasPullRequestSyncEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.PullRequestSync)
}

// HasWikiEvent returns true if hook enabled wiki event.
func (w *Webhook) HasWikiEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvent.Wiki)
}

// HasReleaseEvent returns if hook enabled release event.
func (w *Webhook) HasReleaseEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.Release)
}

// HasRepositoryEvent returns if hook enabled repository event.
func (w *Webhook) HasRepositoryEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.Repository)
}

// HasPackageEvent returns if hook enabled package event.
func (w *Webhook) HasPackageEvent() bool {
	return w.SendEverything ||
		(w.ChooseEvents && w.HookEvents.Package)
}

// EventCheckers returns event checkers
func (w *Webhook) EventCheckers() []struct {
	Has  func() bool
	Type HookEventType
} {
	return []struct {
		Has  func() bool
		Type HookEventType
	}{
		{w.HasCreateEvent, HookEventCreate},
		{w.HasDeleteEvent, HookEventDelete},
		{w.HasForkEvent, HookEventFork},
		{w.HasPushEvent, HookEventPush},
		{w.HasIssuesEvent, HookEventIssues},
		{w.HasIssuesAssignEvent, HookEventIssueAssign},
		{w.HasIssuesLabelEvent, HookEventIssueLabel},
		{w.HasIssuesMilestoneEvent, HookEventIssueMilestone},
		{w.HasIssueCommentEvent, HookEventIssueComment},
		{w.HasPullRequestEvent, HookEventPullRequest},
		{w.HasPullRequestAssignEvent, HookEventPullRequestAssign},
		{w.HasPullRequestLabelEvent, HookEventPullRequestLabel},
		{w.HasPullRequestMilestoneEvent, HookEventPullRequestMilestone},
		{w.HasPullRequestCommentEvent, HookEventPullRequestComment},
		{w.HasPullRequestApprovedEvent, HookEventPullRequestReviewApproved},
		{w.HasPullRequestRejectedEvent, HookEventPullRequestReviewRejected},
		{w.HasPullRequestCommentEvent, HookEventPullRequestReviewComment},
		{w.HasPullRequestSyncEvent, HookEventPullRequestSync},
		{w.HasWikiEvent, HookEventWiki},
		{w.HasRepositoryEvent, HookEventRepository},
		{w.HasReleaseEvent, HookEventRelease},
		{w.HasPackageEvent, HookEventPackage},
	}
}

// EventsArray returns an array of hook events
func (w *Webhook) EventsArray() []string {
	events := make([]string, 0, 7)

	for _, c := range w.EventCheckers() {
		if c.Has() {
			events = append(events, string(c.Type))
		}
	}
	return events
}

// HeaderAuthorization returns the decrypted Authorization header.
// Not on the reference (*w), to be accessible on WebhooksNew.
func (w Webhook) HeaderAuthorization() (string, error) {
	if w.HeaderAuthorizationEncrypted == "" {
		return "", nil
	}
	return secret.DecryptSecret(setting.SecretKey, w.HeaderAuthorizationEncrypted)
}

// SetHeaderAuthorization encrypts and sets the Authorization header.
func (w *Webhook) SetHeaderAuthorization(cleartext string) error {
	if cleartext == "" {
		w.HeaderAuthorizationEncrypted = ""
		return nil
	}
	ciphertext, err := secret.EncryptSecret(setting.SecretKey, cleartext)
	if err != nil {
		return err
	}
	w.HeaderAuthorizationEncrypted = ciphertext
	return nil
}

// CreateWebhook creates a new web hook.
func CreateWebhook(ctx context.Context, w *Webhook) error {
	w.Type = strings.TrimSpace(w.Type)
	return db.Insert(ctx, w)
}

// CreateWebhooks creates multiple web hooks
func CreateWebhooks(ctx context.Context, ws []*Webhook) error {
	// xorm returns err "no element on slice when insert" for empty slices.
	if len(ws) == 0 {
		return nil
	}
	for i := 0; i < len(ws); i++ {
		ws[i].Type = strings.TrimSpace(ws[i].Type)
	}
	return db.Insert(ctx, ws)
}

// getWebhook uses argument bean as query condition,
// ID must be specified and do not assign unnecessary fields.
func getWebhook(bean *Webhook) (*Webhook, error) {
	has, err := db.GetEngine(db.DefaultContext).Get(bean)
	if err != nil {
		return nil, err
	} else if !has {
		return nil, ErrWebhookNotExist{bean.ID}
	}
	return bean, nil
}

// GetWebhookByID returns webhook of repository by given ID.
func GetWebhookByID(id int64) (*Webhook, error) {
	return getWebhook(&Webhook{
		ID: id,
	})
}

// GetWebhookByRepoID returns webhook of repository by given ID.
func GetWebhookByRepoID(repoID, id int64) (*Webhook, error) {
	return getWebhook(&Webhook{
		ID:     id,
		RepoID: repoID,
	})
}

// GetWebhookByOrgID returns webhook of organization by given ID.
func GetWebhookByOrgID(orgID, id int64) (*Webhook, error) {
	return getWebhook(&Webhook{
		ID:    id,
		OrgID: orgID,
	})
}

// ListWebhookOptions are options to filter webhooks on ListWebhooksByOpts
type ListWebhookOptions struct {
	db.ListOptions
	RepoID   int64
	OrgID    int64
	IsActive util.OptionalBool
}

func (opts *ListWebhookOptions) toCond() builder.Cond {
	cond := builder.NewCond()
	if opts.RepoID != 0 {
		cond = cond.And(builder.Eq{"webhook.repo_id": opts.RepoID})
	}
	if opts.OrgID != 0 {
		cond = cond.And(builder.Eq{"webhook.org_id": opts.OrgID})
	}
	if !opts.IsActive.IsNone() {
		cond = cond.And(builder.Eq{"webhook.is_active": opts.IsActive.IsTrue()})
	}
	return cond
}

// ListWebhooksByOpts return webhooks based on options
func ListWebhooksByOpts(ctx context.Context, opts *ListWebhookOptions) ([]*Webhook, error) {
	sess := db.GetEngine(ctx).Where(opts.toCond())

	if opts.Page != 0 {
		sess = db.SetSessionPagination(sess, opts)
		webhooks := make([]*Webhook, 0, opts.PageSize)
		err := sess.Find(&webhooks)
		return webhooks, err
	}

	webhooks := make([]*Webhook, 0, 10)
	err := sess.Find(&webhooks)
	return webhooks, err
}

// CountWebhooksByOpts count webhooks based on options and ignore pagination
func CountWebhooksByOpts(opts *ListWebhookOptions) (int64, error) {
	return db.GetEngine(db.DefaultContext).Where(opts.toCond()).Count(&Webhook{})
}

// GetDefaultWebhooks returns all admin-default webhooks.
func GetDefaultWebhooks(ctx context.Context) ([]*Webhook, error) {
	webhooks := make([]*Webhook, 0, 5)
	return webhooks, db.GetEngine(ctx).
		Where("repo_id=? AND org_id=? AND is_system_webhook=?", 0, 0, false).
		Find(&webhooks)
}

// GetSystemOrDefaultWebhook returns admin system or default webhook by given ID.
func GetSystemOrDefaultWebhook(id int64) (*Webhook, error) {
	webhook := &Webhook{ID: id}
	has, err := db.GetEngine(db.DefaultContext).
		Where("repo_id=? AND org_id=?", 0, 0).
		Get(webhook)
	if err != nil {
		return nil, err
	} else if !has {
		return nil, ErrWebhookNotExist{id}
	}
	return webhook, nil
}

// GetSystemWebhooks returns all admin system webhooks.
func GetSystemWebhooks(ctx context.Context, isActive util.OptionalBool) ([]*Webhook, error) {
	webhooks := make([]*Webhook, 0, 5)
	if isActive.IsNone() {
		return webhooks, db.GetEngine(ctx).
			Where("repo_id=? AND org_id=? AND is_system_webhook=?", 0, 0, true).
			Find(&webhooks)
	}
	return webhooks, db.GetEngine(ctx).
		Where("repo_id=? AND org_id=? AND is_system_webhook=? AND is_active = ?", 0, 0, true, isActive.IsTrue()).
		Find(&webhooks)
}

// UpdateWebhook updates information of webhook.
func UpdateWebhook(w *Webhook) error {
	_, err := db.GetEngine(db.DefaultContext).ID(w.ID).AllCols().Update(w)
	return err
}

// UpdateWebhookLastStatus updates last status of webhook.
func UpdateWebhookLastStatus(w *Webhook) error {
	_, err := db.GetEngine(db.DefaultContext).ID(w.ID).Cols("last_status").Update(w)
	return err
}

// deleteWebhook uses argument bean as query condition,
// ID must be specified and do not assign unnecessary fields.
func deleteWebhook(bean *Webhook) (err error) {
	ctx, committer, err := db.TxContext(db.DefaultContext)
	if err != nil {
		return err
	}
	defer committer.Close()

	if count, err := db.DeleteByBean(ctx, bean); err != nil {
		return err
	} else if count == 0 {
		return ErrWebhookNotExist{ID: bean.ID}
	} else if _, err = db.DeleteByBean(ctx, &HookTask{HookID: bean.ID}); err != nil {
		return err
	}

	return committer.Commit()
}

// DeleteWebhookByRepoID deletes webhook of repository by given ID.
func DeleteWebhookByRepoID(repoID, id int64) error {
	return deleteWebhook(&Webhook{
		ID:     id,
		RepoID: repoID,
	})
}

// DeleteWebhookByOrgID deletes webhook of organization by given ID.
func DeleteWebhookByOrgID(orgID, id int64) error {
	return deleteWebhook(&Webhook{
		ID:    id,
		OrgID: orgID,
	})
}

// DeleteDefaultSystemWebhook deletes an admin-configured default or system webhook (where Org and Repo ID both 0)
func DeleteDefaultSystemWebhook(id int64) error {
	ctx, committer, err := db.TxContext(db.DefaultContext)
	if err != nil {
		return err
	}
	defer committer.Close()

	count, err := db.GetEngine(ctx).
		Where("repo_id=? AND org_id=?", 0, 0).
		Delete(&Webhook{ID: id})
	if err != nil {
		return err
	} else if count == 0 {
		return ErrWebhookNotExist{ID: id}
	}

	if _, err := db.DeleteByBean(ctx, &HookTask{HookID: id}); err != nil {
		return err
	}

	return committer.Commit()
}

// CopyDefaultWebhooksToRepo creates copies of the default webhooks in a new repo
func CopyDefaultWebhooksToRepo(ctx context.Context, repoID int64) error {
	ws, err := GetDefaultWebhooks(ctx)
	if err != nil {
		return fmt.Errorf("GetDefaultWebhooks: %w", err)
	}

	for _, w := range ws {
		w.ID = 0
		w.RepoID = repoID
		if err := CreateWebhook(ctx, w); err != nil {
			return fmt.Errorf("CreateWebhook: %w", err)
		}
	}
	return nil
}
