// Copyright 2017 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package models

import (
	"html/template"
	"testing"

	"code.gitea.io/gitea/models/db"
	repo_model "code.gitea.io/gitea/models/repo"
	"code.gitea.io/gitea/models/unittest"
	user_model "code.gitea.io/gitea/models/user"

	"github.com/stretchr/testify/assert"
)

// TODO TestGetLabelTemplateFile

func TestLabel_CalOpenIssues(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	label := unittest.AssertExistsAndLoadBean(t, &Label{ID: 1}).(*Label)
	label.CalOpenIssues()
	assert.EqualValues(t, 2, label.NumOpenIssues)
}

func TestLabel_ForegroundColor(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	label := unittest.AssertExistsAndLoadBean(t, &Label{ID: 1}).(*Label)
	assert.Equal(t, template.CSS("#000"), label.ForegroundColor())

	label = unittest.AssertExistsAndLoadBean(t, &Label{ID: 2}).(*Label)
	assert.Equal(t, template.CSS("#fff"), label.ForegroundColor())
}

func TestNewLabels(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	labels := []*Label{
		{RepoID: 2, Name: "labelName2", Color: "#123456"},
		{RepoID: 3, Name: "labelName3", Color: "#123"},
		{RepoID: 4, Name: "labelName4", Color: "ABCDEF"},
		{RepoID: 5, Name: "labelName5", Color: "DEF"},
	}
	assert.Error(t, NewLabel(&Label{RepoID: 3, Name: "invalid Color", Color: ""}))
	assert.Error(t, NewLabel(&Label{RepoID: 3, Name: "invalid Color", Color: "#45G"}))
	assert.Error(t, NewLabel(&Label{RepoID: 3, Name: "invalid Color", Color: "#12345G"}))
	assert.Error(t, NewLabel(&Label{RepoID: 3, Name: "invalid Color", Color: "45G"}))
	assert.Error(t, NewLabel(&Label{RepoID: 3, Name: "invalid Color", Color: "12345G"}))
	for _, label := range labels {
		unittest.AssertNotExistsBean(t, label)
	}
	assert.NoError(t, NewLabels(labels...))
	for _, label := range labels {
		unittest.AssertExistsAndLoadBean(t, label, unittest.Cond("id = ?", label.ID))
	}
	unittest.CheckConsistencyFor(t, &Label{}, &repo_model.Repository{})
}

func TestGetLabelByID(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	label, err := GetLabelByID(1)
	assert.NoError(t, err)
	assert.EqualValues(t, 1, label.ID)

	_, err = GetLabelByID(unittest.NonexistentID)
	assert.True(t, IsErrLabelNotExist(err))
}

func TestGetLabelInRepoByName(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	label, err := GetLabelInRepoByName(1, "label1")
	assert.NoError(t, err)
	assert.EqualValues(t, 1, label.ID)
	assert.Equal(t, "label1", label.Name)

	_, err = GetLabelInRepoByName(1, "")
	assert.True(t, IsErrRepoLabelNotExist(err))

	_, err = GetLabelInRepoByName(unittest.NonexistentID, "nonexistent")
	assert.True(t, IsErrRepoLabelNotExist(err))
}

func TestGetLabelInRepoByNames(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	labelIDs, err := GetLabelIDsInRepoByNames(1, []string{"label1", "label2"})
	assert.NoError(t, err)

	assert.Len(t, labelIDs, 2)

	assert.Equal(t, int64(1), labelIDs[0])
	assert.Equal(t, int64(2), labelIDs[1])
}

func TestGetLabelInRepoByNamesDiscardsNonExistentLabels(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	// label3 doesn't exists.. See labels.yml
	labelIDs, err := GetLabelIDsInRepoByNames(1, []string{"label1", "label2", "label3"})
	assert.NoError(t, err)

	assert.Len(t, labelIDs, 2)

	assert.Equal(t, int64(1), labelIDs[0])
	assert.Equal(t, int64(2), labelIDs[1])
	assert.NoError(t, err)
}

func TestGetLabelInRepoByID(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	label, err := GetLabelInRepoByID(1, 1)
	assert.NoError(t, err)
	assert.EqualValues(t, 1, label.ID)

	_, err = GetLabelInRepoByID(1, -1)
	assert.True(t, IsErrRepoLabelNotExist(err))

	_, err = GetLabelInRepoByID(unittest.NonexistentID, unittest.NonexistentID)
	assert.True(t, IsErrRepoLabelNotExist(err))
}

func TestGetLabelsInRepoByIDs(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	labels, err := GetLabelsInRepoByIDs(1, []int64{1, 2, unittest.NonexistentID})
	assert.NoError(t, err)
	if assert.Len(t, labels, 2) {
		assert.EqualValues(t, 1, labels[0].ID)
		assert.EqualValues(t, 2, labels[1].ID)
	}
}

func TestGetLabelsByRepoID(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	testSuccess := func(repoID int64, sortType string, expectedIssueIDs []int64) {
		labels, err := GetLabelsByRepoID(repoID, sortType, db.ListOptions{})
		assert.NoError(t, err)
		assert.Len(t, labels, len(expectedIssueIDs))
		for i, label := range labels {
			assert.EqualValues(t, expectedIssueIDs[i], label.ID)
		}
	}
	testSuccess(1, "leastissues", []int64{2, 1})
	testSuccess(1, "mostissues", []int64{1, 2})
	testSuccess(1, "reversealphabetically", []int64{2, 1})
	testSuccess(1, "default", []int64{1, 2})
}

// Org versions

func TestGetLabelInOrgByName(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	label, err := GetLabelInOrgByName(3, "orglabel3")
	assert.NoError(t, err)
	assert.EqualValues(t, 3, label.ID)
	assert.Equal(t, "orglabel3", label.Name)

	_, err = GetLabelInOrgByName(3, "")
	assert.True(t, IsErrOrgLabelNotExist(err))

	_, err = GetLabelInOrgByName(0, "orglabel3")
	assert.True(t, IsErrOrgLabelNotExist(err))

	_, err = GetLabelInOrgByName(-1, "orglabel3")
	assert.True(t, IsErrOrgLabelNotExist(err))

	_, err = GetLabelInOrgByName(unittest.NonexistentID, "nonexistent")
	assert.True(t, IsErrOrgLabelNotExist(err))
}

func TestGetLabelInOrgByNames(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	labelIDs, err := GetLabelIDsInOrgByNames(3, []string{"orglabel3", "orglabel4"})
	assert.NoError(t, err)

	assert.Len(t, labelIDs, 2)

	assert.Equal(t, int64(3), labelIDs[0])
	assert.Equal(t, int64(4), labelIDs[1])
}

func TestGetLabelInOrgByNamesDiscardsNonExistentLabels(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	// orglabel99 doesn't exists.. See labels.yml
	labelIDs, err := GetLabelIDsInOrgByNames(3, []string{"orglabel3", "orglabel4", "orglabel99"})
	assert.NoError(t, err)

	assert.Len(t, labelIDs, 2)

	assert.Equal(t, int64(3), labelIDs[0])
	assert.Equal(t, int64(4), labelIDs[1])
	assert.NoError(t, err)
}

func TestGetLabelInOrgByID(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	label, err := GetLabelInOrgByID(3, 3)
	assert.NoError(t, err)
	assert.EqualValues(t, 3, label.ID)

	_, err = GetLabelInOrgByID(3, -1)
	assert.True(t, IsErrOrgLabelNotExist(err))

	_, err = GetLabelInOrgByID(0, 3)
	assert.True(t, IsErrOrgLabelNotExist(err))

	_, err = GetLabelInOrgByID(-1, 3)
	assert.True(t, IsErrOrgLabelNotExist(err))

	_, err = GetLabelInOrgByID(unittest.NonexistentID, unittest.NonexistentID)
	assert.True(t, IsErrOrgLabelNotExist(err))
}

func TestGetLabelsInOrgByIDs(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	labels, err := GetLabelsInOrgByIDs(3, []int64{3, 4, unittest.NonexistentID})
	assert.NoError(t, err)
	if assert.Len(t, labels, 2) {
		assert.EqualValues(t, 3, labels[0].ID)
		assert.EqualValues(t, 4, labels[1].ID)
	}
}

func TestGetLabelsByOrgID(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	testSuccess := func(orgID int64, sortType string, expectedIssueIDs []int64) {
		labels, err := GetLabelsByOrgID(orgID, sortType, db.ListOptions{})
		assert.NoError(t, err)
		assert.Len(t, labels, len(expectedIssueIDs))
		for i, label := range labels {
			assert.EqualValues(t, expectedIssueIDs[i], label.ID)
		}
	}
	testSuccess(3, "leastissues", []int64{3, 4})
	testSuccess(3, "mostissues", []int64{4, 3})
	testSuccess(3, "reversealphabetically", []int64{4, 3})
	testSuccess(3, "default", []int64{3, 4})

	var err error
	_, err = GetLabelsByOrgID(0, "leastissues", db.ListOptions{})
	assert.True(t, IsErrOrgLabelNotExist(err))

	_, err = GetLabelsByOrgID(-1, "leastissues", db.ListOptions{})
	assert.True(t, IsErrOrgLabelNotExist(err))
}

//

func TestGetLabelsByIssueID(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	labels, err := GetLabelsByIssueID(1)
	assert.NoError(t, err)
	if assert.Len(t, labels, 1) {
		assert.EqualValues(t, 1, labels[0].ID)
	}

	labels, err = GetLabelsByIssueID(unittest.NonexistentID)
	assert.NoError(t, err)
	assert.Len(t, labels, 0)
}

func TestUpdateLabel(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	label := unittest.AssertExistsAndLoadBean(t, &Label{ID: 1}).(*Label)
	// make sure update wont overwrite it
	update := &Label{
		ID:          label.ID,
		Color:       "#ffff00",
		Name:        "newLabelName",
		Description: label.Description,
	}
	label.Color = update.Color
	label.Name = update.Name
	assert.NoError(t, UpdateLabel(update))
	newLabel := unittest.AssertExistsAndLoadBean(t, &Label{ID: 1}).(*Label)
	assert.EqualValues(t, label.ID, newLabel.ID)
	assert.EqualValues(t, label.Color, newLabel.Color)
	assert.EqualValues(t, label.Name, newLabel.Name)
	assert.EqualValues(t, label.Description, newLabel.Description)
	unittest.CheckConsistencyFor(t, &Label{}, &repo_model.Repository{})
}

func TestDeleteLabel(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	label := unittest.AssertExistsAndLoadBean(t, &Label{ID: 1}).(*Label)
	assert.NoError(t, DeleteLabel(label.RepoID, label.ID))
	unittest.AssertNotExistsBean(t, &Label{ID: label.ID, RepoID: label.RepoID})

	assert.NoError(t, DeleteLabel(label.RepoID, label.ID))
	unittest.AssertNotExistsBean(t, &Label{ID: label.ID})

	assert.NoError(t, DeleteLabel(unittest.NonexistentID, unittest.NonexistentID))
	unittest.CheckConsistencyFor(t, &Label{}, &repo_model.Repository{})
}

func TestHasIssueLabel(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	assert.True(t, HasIssueLabel(1, 1))
	assert.False(t, HasIssueLabel(1, 2))
	assert.False(t, HasIssueLabel(unittest.NonexistentID, unittest.NonexistentID))
}

func TestNewIssueLabel(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	label := unittest.AssertExistsAndLoadBean(t, &Label{ID: 2}).(*Label)
	issue := unittest.AssertExistsAndLoadBean(t, &Issue{ID: 1}).(*Issue)
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}).(*user_model.User)

	// add new IssueLabel
	prevNumIssues := label.NumIssues
	assert.NoError(t, NewIssueLabel(issue, label, doer))
	unittest.AssertExistsAndLoadBean(t, &IssueLabel{IssueID: issue.ID, LabelID: label.ID})
	unittest.AssertExistsAndLoadBean(t, &Comment{
		Type:     CommentTypeLabel,
		PosterID: doer.ID,
		IssueID:  issue.ID,
		LabelID:  label.ID,
		Content:  "1",
	})
	label = unittest.AssertExistsAndLoadBean(t, &Label{ID: 2}).(*Label)
	assert.EqualValues(t, prevNumIssues+1, label.NumIssues)

	// re-add existing IssueLabel
	assert.NoError(t, NewIssueLabel(issue, label, doer))
	unittest.CheckConsistencyFor(t, &Issue{}, &Label{})
}

func TestNewIssueLabels(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	label1 := unittest.AssertExistsAndLoadBean(t, &Label{ID: 1}).(*Label)
	label2 := unittest.AssertExistsAndLoadBean(t, &Label{ID: 2}).(*Label)
	issue := unittest.AssertExistsAndLoadBean(t, &Issue{ID: 5}).(*Issue)
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}).(*user_model.User)

	assert.NoError(t, NewIssueLabels(issue, []*Label{label1, label2}, doer))
	unittest.AssertExistsAndLoadBean(t, &IssueLabel{IssueID: issue.ID, LabelID: label1.ID})
	unittest.AssertExistsAndLoadBean(t, &Comment{
		Type:     CommentTypeLabel,
		PosterID: doer.ID,
		IssueID:  issue.ID,
		LabelID:  label1.ID,
		Content:  "1",
	})
	unittest.AssertExistsAndLoadBean(t, &IssueLabel{IssueID: issue.ID, LabelID: label1.ID})
	label1 = unittest.AssertExistsAndLoadBean(t, &Label{ID: 1}).(*Label)
	assert.EqualValues(t, 3, label1.NumIssues)
	assert.EqualValues(t, 1, label1.NumClosedIssues)
	label2 = unittest.AssertExistsAndLoadBean(t, &Label{ID: 2}).(*Label)
	assert.EqualValues(t, 1, label2.NumIssues)
	assert.EqualValues(t, 1, label2.NumClosedIssues)

	// corner case: test empty slice
	assert.NoError(t, NewIssueLabels(issue, []*Label{}, doer))

	unittest.CheckConsistencyFor(t, &Issue{}, &Label{})
}

func TestDeleteIssueLabel(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	testSuccess := func(labelID, issueID, doerID int64) {
		label := unittest.AssertExistsAndLoadBean(t, &Label{ID: labelID}).(*Label)
		issue := unittest.AssertExistsAndLoadBean(t, &Issue{ID: issueID}).(*Issue)
		doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: doerID}).(*user_model.User)

		expectedNumIssues := label.NumIssues
		expectedNumClosedIssues := label.NumClosedIssues
		if unittest.BeanExists(t, &IssueLabel{IssueID: issueID, LabelID: labelID}) {
			expectedNumIssues--
			if issue.IsClosed {
				expectedNumClosedIssues--
			}
		}

		assert.NoError(t, DeleteIssueLabel(issue, label, doer))
		unittest.AssertNotExistsBean(t, &IssueLabel{IssueID: issueID, LabelID: labelID})
		unittest.AssertExistsAndLoadBean(t, &Comment{
			Type:     CommentTypeLabel,
			PosterID: doerID,
			IssueID:  issueID,
			LabelID:  labelID,
		}, `content=""`)
		label = unittest.AssertExistsAndLoadBean(t, &Label{ID: labelID}).(*Label)
		assert.EqualValues(t, expectedNumIssues, label.NumIssues)
		assert.EqualValues(t, expectedNumClosedIssues, label.NumClosedIssues)
	}
	testSuccess(1, 1, 2)
	testSuccess(2, 5, 2)
	testSuccess(1, 1, 2) // delete non-existent IssueLabel

	unittest.CheckConsistencyFor(t, &Issue{}, &Label{})
}
